package security

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type Limits struct {
	MaxString int // generic string max length (e.g., flag values, user fields)
	MaxPath   int // file path max length
	AllowNL   bool
	AllowTab  bool
}

func DefaultLimits() Limits {
	return Limits{
		MaxString: 4096,
		MaxPath:   4096,
		AllowNL:   true,
		AllowTab:  true,
	}
}

// ---------- primitive checks ----------

func ValidateString(name, s string, lim Limits) error {
	if s == "" {
		return nil
	}
	if !utf8.ValidString(s) {
		return fmt.Errorf("%s: invalid UTF-8", name)
	}
	if strings.ContainsRune(s, '\x00') {
		return fmt.Errorf("%s: contains NUL byte", name)
	}
	if utf8.RuneCountInString(s) > lim.MaxString {
		return fmt.Errorf("%s: too long (%d > %d)", name, utf8.RuneCountInString(s), lim.MaxString)
	}
	for _, r := range s {
		if r == '\n' && lim.AllowNL {
			continue
		}
		if r == '\t' && lim.AllowTab {
			continue
		}
		if !unicode.IsPrint(r) {
			return fmt.Errorf("%s: contains non-printable/control runes", name)
		}
	}
	return nil
}

func ValidatePath(name, s string, lim Limits) error {
	if err := ValidateString(name, s, lim); err != nil {
		return err
	}
	_ = filepath.Clean(s) // keep behavior stable; we only validate, not mutate
	return nil
}

// ---------- struct-wide (config) validation ----------

func ValidateStructStrings(obj any, lim Limits) error {
	seen := map[uintptr]bool{}
	return walkValue(reflect.ValueOf(obj), "config", lim, seen)
}

func walkValue(v reflect.Value, path string, lim Limits, seen map[uintptr]bool) error {
	if !v.IsValid() {
		return nil
	}
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return nil
		}
		ptr := v.Pointer()
		if seen[ptr] {
			return nil
		}
		seen[ptr] = true
		return walkValue(v.Elem(), path, lim, seen)

	case reflect.Interface:
		if v.IsNil() {
			return nil
		}
		return walkValue(v.Elem(), path, lim, seen)

	case reflect.Struct:
		t := v.Type()
		for i := 0; i < v.NumField(); i++ {
			f := v.Field(i)
			name := t.Field(i).Name
			if !f.CanInterface() {
				continue
			}
			if err := walkValue(f, path+"."+name, lim, seen); err != nil {
				return err
			}
		}
	case reflect.Map:
		for _, k := range v.MapKeys() {
			val := v.MapIndex(k)
			if err := walkValue(val, path+"["+fmt.Sprint(k.Interface())+"]", lim, seen); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			if err := walkValue(v.Index(i), fmt.Sprintf("%s[%d]", path, i), lim, seen); err != nil {
				return err
			}
		}
	case reflect.String:
		s := v.String()
		// Heuristic: treat fields named "*Path" or "*File*" as paths
		lower := strings.ToLower(path)
		if strings.Contains(lower, "path") || strings.Contains(lower, "file") {
			return ValidatePath(path, s, lim)
		}
		return ValidateString(path, s, lim)
	}
	return nil
}

// ---------- Cobra integration ----------

func AttachRecursive(root *cobra.Command, lim Limits) {
	attach(root, lim)
	for _, c := range root.Commands() {
		AttachRecursive(c, lim)
	}
}

func attach(cmd *cobra.Command, lim Limits) {
	prev := cmd.PersistentPreRunE
	cmd.PersistentPreRunE = func(c *cobra.Command, args []string) error {
		if err := validateFlagsAndArgs(c, args, lim); err != nil {
			return err
		}
		if prev != nil {
			return prev(c, args)
		}
		return nil
	}
}

func validateFlagsAndArgs(cmd *cobra.Command, args []string, lim Limits) error {
	// Arguments
	for i, a := range args {
		if err := ValidateString(fmt.Sprintf("arg[%d]", i), a, lim); err != nil {
			return err
		}
	}

	// Flags (string, stringSlice, stringArray)
	var firstErr error
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		if firstErr != nil {
			return
		}
		typ := f.Value.Type()
		name := fmt.Sprintf("flag --%s", f.Name)

		isPathy := strings.Contains(strings.ToLower(f.Name), "path") ||
			strings.Contains(strings.ToLower(f.Name), "file")

		switch typ {
		case "string":
			val, _ := cmd.Flags().GetString(f.Name)
			if val == "" {
				return
			}
			if isPathy {
				firstErr = ValidatePath(name, val, lim)
			} else {
				firstErr = ValidateString(name, val, lim)
			}
		case "stringSlice":
			vals, _ := cmd.Flags().GetStringSlice(f.Name)
			for i, v := range vals {
				if v == "" {
					continue
				}
				if isPathy {
					firstErr = ValidatePath(fmt.Sprintf("%s[%d]", name, i), v, lim)
				} else {
					firstErr = ValidateString(fmt.Sprintf("%s[%d]", name, i), v, lim)
				}
				if firstErr != nil {
					return
				}
			}
		case "stringArray":
			vals, _ := cmd.Flags().GetStringArray(f.Name)
			for i, v := range vals {
				if v == "" {
					continue
				}
				if isPathy {
					firstErr = ValidatePath(fmt.Sprintf("%s[%d]", name, i), v, lim)
				} else {
					firstErr = ValidateString(fmt.Sprintf("%s[%d]", name, i), v, lim)
				}
				if firstErr != nil {
					return
				}
			}
		default:
			// other flag types ignored for this control/UTF-8/length check
		}
	})
	return firstErr
}
