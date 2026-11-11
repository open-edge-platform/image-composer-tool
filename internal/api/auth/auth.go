// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// Package auth says who is calling the API.
//
// It is deliberately small. ICT does not perform logins: a deployment that
// needs sign-in puts an authenticating reverse proxy in front of `serve` (an
// OIDC proxy, an enterprise gateway) and the proxy tells ICT who the user is
// through request headers. ICT then attributes builds to that user and shows
// each user their own history. Without a proxy, every request is one fixed
// local user, which is today's behaviour.
//
// docs/user-guide/architecture/web-ui-caller-identity.md shows the package as a
// class diagram and walks through one request.
package auth

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
)

// Identity is the authenticated caller.
type Identity struct {
	// Subject is the stable, unique key the proxy reports (a user name, an
	// object id). Builds are filed under it.
	Subject string
	Name    string
	Email   string
	Groups  []string
}

// Owner is the key builds are attributed to and history is filtered by.
func (i Identity) Owner() string { return i.Subject }

// DisplayName is the friendliest name available.
func (i Identity) DisplayName() string {
	switch {
	case i.Name != "":
		return i.Name
	case i.Email != "":
		return i.Email
	default:
		return i.Subject
	}
}

// Provider identifies the caller of a request.
type Provider interface {
	// Mode names the provider for GET /api/v1/server: "none" or "trusted-header".
	Mode() string
	// Identify returns the caller's identity. An error means the request carries
	// no acceptable identity and must be refused with 401.
	Identify(r *http.Request) (Identity, error)
}

// None attributes every request to one fixed user. It is the default, and the
// right choice for `serve` on a developer's own machine.
type None struct {
	User string
}

// Mode implements Provider.
func (None) Mode() string { return "none" }

// Identify implements Provider.
func (n None) Identify(*http.Request) (Identity, error) {
	user := n.User
	if user == "" {
		user = "local"
	}
	return Identity{Subject: user, Name: user}, nil
}

// TrustedHeader reads the identity an authenticating reverse proxy put in the
// request headers. The proxy must be the only way to reach the server: bind
// `serve` to loopback inside the same pod or host, and let nothing else route to
// it, or anyone able to send a request can claim any identity by setting the
// header. TrustedProxies narrows that further to requests whose peer address is
// in one of the given networks.
type TrustedHeader struct {
	// UserHeader is required on every request; its value is the Subject.
	// Default "X-Forwarded-User".
	UserHeader string
	// NameHeader, EmailHeader and GroupsHeader are optional. Defaults
	// "X-Forwarded-Preferred-Username", "X-Forwarded-Email", "X-Forwarded-Groups".
	NameHeader   string
	EmailHeader  string
	GroupsHeader string
	// GroupsSeparator splits GroupsHeader; default ",".
	GroupsSeparator string
	// TrustedProxies, when set, lists the CIDRs a request's peer must be in.
	TrustedProxies []*net.IPNet
}

// Mode implements Provider.
func (TrustedHeader) Mode() string { return "trusted-header" }

// ErrNoIdentity is returned when the proxy header is missing.
var ErrNoIdentity = errors.New("no identity header on the request")

// ErrUntrustedPeer is returned when the request did not come through a trusted proxy.
var ErrUntrustedPeer = errors.New("request did not arrive from a trusted proxy")

// Identify implements Provider.
func (t TrustedHeader) Identify(r *http.Request) (Identity, error) {
	if len(t.TrustedProxies) > 0 {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		ip := net.ParseIP(host)
		trusted := false
		for _, n := range t.TrustedProxies {
			if ip != nil && n.Contains(ip) {
				trusted = true
				break
			}
		}
		if !trusted {
			return Identity{}, ErrUntrustedPeer
		}
	}
	user := strings.TrimSpace(r.Header.Get(t.header(t.UserHeader, "X-Forwarded-User")))
	if user == "" {
		return Identity{}, ErrNoIdentity
	}
	id := Identity{
		Subject: user,
		Name:    strings.TrimSpace(r.Header.Get(t.header(t.NameHeader, "X-Forwarded-Preferred-Username"))),
		Email:   strings.TrimSpace(r.Header.Get(t.header(t.EmailHeader, "X-Forwarded-Email"))),
	}
	sep := t.GroupsSeparator
	if sep == "" {
		sep = ","
	}
	for _, g := range strings.Split(r.Header.Get(t.header(t.GroupsHeader, "X-Forwarded-Groups")), sep) {
		if g = strings.TrimSpace(g); g != "" {
			id.Groups = append(id.Groups, g)
		}
	}
	return id, nil
}

func (TrustedHeader) header(name, def string) string {
	if name == "" {
		return def
	}
	return name
}

// ParseCIDRs parses a comma-separated list of CIDRs or single addresses.
func ParseCIDRs(list string) ([]*net.IPNet, error) {
	var out []*net.IPNet
	for _, item := range strings.Split(list, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if !strings.Contains(item, "/") {
			if ip := net.ParseIP(item); ip != nil {
				bits := 32
				if ip.To4() == nil {
					bits = 128
				}
				item = fmt.Sprintf("%s/%d", ip, bits)
			}
		}
		_, n, err := net.ParseCIDR(item)
		if err != nil {
			return nil, fmt.Errorf("trusted proxy %q: %w", item, err)
		}
		out = append(out, n)
	}
	return out, nil
}

type ctxKey struct{}

// WithIdentity returns ctx carrying id.
func WithIdentity(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

// FromContext returns the identity Middleware attached, if any.
func FromContext(ctx context.Context) (Identity, bool) {
	id, ok := ctx.Value(ctxKey{}).(Identity)
	return id, ok
}

// Middleware identifies every request with p and attaches the identity to the
// context, or refuses the request with 401 and the given error writer.
func Middleware(p Provider, unauthorized func(w http.ResponseWriter, msg string), next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := p.Identify(r)
		if err != nil {
			unauthorized(w, err.Error())
			return
		}
		next.ServeHTTP(w, r.WithContext(WithIdentity(r.Context(), id)))
	})
}
