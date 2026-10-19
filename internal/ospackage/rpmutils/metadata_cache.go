package rpmutils

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"net/textproto"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type rpmPrimaryReference struct {
	Href string
	// ChecksumType is the repomd.xml checksum type in its canonical upper-case
	// form (for example "SHA256"); repomd.xml itself usually spells it in
	// lower case.
	ChecksumType string
	Checksum     string
}

func (ref rpmPrimaryReference) isPartialChecksum() bool {
	return (ref.ChecksumType != "" && ref.Checksum == "") || (ref.ChecksumType == "" && ref.Checksum != "")
}

func redactURLForLog(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Opaque != "" || u.Scheme == "" || u.Host == "" {
		return redactedURLPlaceholder
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

// redactedURLPlaceholder stands in for a URL that cannot be safely reduced to
// scheme and host.
const redactedURLPlaceholder = "<redacted-url>"

// malformedHeaderPlaceholder replaces the diagnostic for a malformed protocol
// message, which echoes the offending header line.
const malformedHeaderPlaceholder = "malformed MIME header (details redacted)"

// urlInTextPattern finds URL-like text in an error message, including text
// quoted by %q, whose escape sequences are consumed as pairs.
var urlInTextPattern = regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.\-]*://(?:[^\s"\\]|\\.)*`)

// quotedTargetPattern finds the quoted target in the diagnostics net/http and
// net/url print for a reference they could not parse, as in `failed to parse
// Location header "..."` and `parse "..."`. The target may be relative or
// protocol-relative, which urlInTextPattern cannot recognise.
var quotedTargetPattern = regexp.MustCompile(`(Location header |\bparse )"(?:[^"\\]|\\.)*"`)

// quotedInner is s as %q would print it, without the surrounding quotes.
func quotedInner(s string) string {
	quoted := strconv.Quote(s)
	return quoted[1 : len(quoted)-1]
}

// redactedError displays only the sanitized message but unwraps to the original
// error, so errors.Is and errors.As keep working on the cause (for example
// context.DeadlineExceeded or a *net.OpError). Callers must not print values
// obtained by unwrapping it.
type redactedError struct {
	msg string
	err error
}

func (e *redactedError) Error() string { return e.msg }
func (e *redactedError) Unwrap() error { return e.err }

// walkErrorChain calls visit for every error in err's chain, walking both
// Unwrap() error and Unwrap() []error (errors.Join, fmt.Errorf with several %w)
// depth-first.
func walkErrorChain(err error, visit func(error)) {
	if err == nil {
		return
	}
	visit(err)
	switch w := err.(type) {
	case interface{ Unwrap() []error }:
		for _, inner := range w.Unwrap() {
			walkErrorChain(inner, visit)
		}
	case interface{ Unwrap() error }:
		walkErrorChain(w.Unwrap(), visit)
	}
}

// collectErrorURLs returns the URL of every *url.Error in err's chain.
func collectErrorURLs(err error) []string {
	var urls []string
	walkErrorChain(err, func(e error) {
		if urlErr, ok := e.(*url.Error); ok {
			urls = append(urls, urlErr.URL)
		}
	})
	return urls
}

// collectProtocolDiagnostics returns the message of every textproto.ProtocolError
// in err's chain. These quote the offending header line verbatim, so a malformed
// Location or Set-Cookie header would leak its value.
func collectProtocolDiagnostics(err error) []string {
	var diagnostics []string
	walkErrorChain(err, func(e error) {
		if protoErr, ok := e.(textproto.ProtocolError); ok {
			diagnostics = append(diagnostics, string(protoErr))
		}
	})
	return diagnostics
}

// replaceKnownURLs replaces each URL in olds (longest first) with its entry in
// replacements. Text that continues a known URL's query or fragment (for example
// "&signature=...") belongs to the same URL and goes with it.
func replaceKnownURLs(message string, olds []string, replacements map[string]string) string {
	if len(olds) == 0 {
		return message
	}
	quotedOlds := make([]string, len(olds))
	for i, old := range olds {
		quotedOlds[i] = regexp.QuoteMeta(old)
	}
	knownURLs := regexp.MustCompile("(?:" + strings.Join(quotedOlds, "|") + `)(?:[&;#](?:[^\s"\\]|\\.)*)?`)
	return knownURLs.ReplaceAllStringFunc(message, func(match string) string {
		for _, old := range olds {
			if strings.HasPrefix(match, old) {
				return replacements[old]
			}
		}
		return match
	})
}

// redactErrorURL removes credentials, query strings and fragments from every URL
// an error mentions. It replaces the requested URL and the URLs carried by
// *url.Error values in the chain (redirect targets, and the form with the
// password masked by net/http), in the plain and the %q-quoted form url.Error
// prints, taking the longest match at each position. URLs that appear only in
// unstructured text, such as a malformed Location header (absolute, relative or
// protocol-relative), are found by pattern. Protocol diagnostics that quote a
// header line are replaced wholesale.
func redactErrorURL(err error, rawURL string) error {
	if err == nil {
		return nil
	}

	candidates := append([]string{rawURL}, collectErrorURLs(err)...)

	replacements := map[string]string{}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		redacted := redactURLForLog(candidate)
		replacements[candidate] = redacted
		replacements[quotedInner(candidate)] = quotedInner(redacted)
		if u, parseErr := url.Parse(candidate); parseErr == nil && u.Opaque == "" {
			replacements[u.String()] = redacted
		}
	}
	olds := make([]string, 0, len(replacements))
	for old := range replacements {
		olds = append(olds, old)
	}
	sort.Slice(olds, func(i, j int) bool { return len(olds[i]) > len(olds[j]) })
	message := err.Error()
	for _, diagnostic := range collectProtocolDiagnostics(err) {
		message = strings.ReplaceAll(message, diagnostic, malformedHeaderPlaceholder)
	}
	message = replaceKnownURLs(message, olds, replacements)
	message = quotedTargetPattern.ReplaceAllString(message, `${1}"`+redactedURLPlaceholder+`"`)
	message = urlInTextPattern.ReplaceAllStringFunc(message, redactURLForLog)
	if message == err.Error() {
		return err
	}
	return &redactedError{msg: message, err: err}
}

func extractPrimaryReferenceFromRepomdData(repomdData []byte) (rpmPrimaryReference, error) {
	dec := xml.NewDecoder(bytes.NewReader(repomdData))

	// Walk the tokens looking for <data type="primary">
	for {
		tok, err := dec.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return rpmPrimaryReference{}, err
		}

		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "data" {
			continue
		}

		var isPrimary bool
		for _, attr := range se.Attr {
			if attr.Name.Local == "type" && attr.Value == "primary" {
				isPrimary = true
				break
			}
		}
		if !isPrimary {
			if err := dec.Skip(); err != nil {
				return rpmPrimaryReference{}, fmt.Errorf("error skipping token: %w", err)
			}
			continue
		}

		primary := rpmPrimaryReference{}
		for {
			tok2, err := dec.Token()
			if err != nil {
				if err == io.EOF {
					err = io.ErrUnexpectedEOF
				}
				return rpmPrimaryReference{}, fmt.Errorf("read primary data in repomd.xml: %w", err)
			}

			if ee, ok := tok2.(xml.EndElement); ok && ee.Name.Local == "data" {
				if primary.Href == "" {
					return rpmPrimaryReference{}, fmt.Errorf("primary location not found in repomd.xml")
				}
				if primary.isPartialChecksum() {
					return rpmPrimaryReference{}, fmt.Errorf("malformed repomd: partial checksum (type=%q, value=%q)", primary.ChecksumType, primary.Checksum)
				}
				primary.ChecksumType = strings.ToUpper(primary.ChecksumType)
				return primary, nil
			}

			le, ok := tok2.(xml.StartElement)
			if !ok {
				continue
			}
			switch le.Name.Local {
			case "location":
				for _, attr := range le.Attr {
					if attr.Name.Local == "href" {
						primary.Href = attr.Value
					}
				}
			case "checksum":
				primary.ChecksumType = attrValue(le.Attr, "type")
				text, err := readElementText(dec)
				if err != nil {
					return rpmPrimaryReference{}, fmt.Errorf("read primary checksum: %w", err)
				}
				primary.Checksum = text
			}
		}
	}

	return rpmPrimaryReference{}, fmt.Errorf("primary location not found in repomd.xml")
}

func attrValue(attrs []xml.Attr, name string) string {
	for _, attr := range attrs {
		if attr.Name.Local == name {
			return attr.Value
		}
	}
	return ""
}

func readElementText(dec *xml.Decoder) (string, error) {
	var text strings.Builder
	depth := 0
	for {
		tok, err := dec.Token()
		if err != nil {
			return "", err
		}
		switch t := tok.(type) {
		case xml.CharData:
			if depth == 0 {
				text.Write(t)
			}
		case xml.StartElement:
			depth++
		case xml.EndElement:
			if depth == 0 {
				return strings.TrimSpace(text.String()), nil
			}
			depth--
		}
	}
}
