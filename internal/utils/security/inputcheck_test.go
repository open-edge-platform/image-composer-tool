package security

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestValidateString_Basics(t *testing.T) {
	lim := DefaultLimits()
	if err := ValidateString("ok", "hello", lim); err != nil {
		t.Fatal(err)
	}
	if err := ValidateString("nul", "a\x00b", lim); err == nil {
		t.Fatal("expected NUL reject")
	}
	if err := ValidateString("nonprint", "a\u0007b", lim); err == nil {
		t.Fatal("expected control char reject")
	}
	if err := ValidateString("badutf8", string([]byte{0xff, 0xfe, 0xfd}), lim); err == nil {
		t.Fatal("expected invalid UTF-8 reject")
	}
}
func TestValidateString_EmptyAndLength(t *testing.T) {
	lim := DefaultLimits()
	if err := ValidateString("empty", "", lim); err != nil {
		t.Fatalf("empty string should be valid: %v", err)
	}
	long := strings.Repeat("a", lim.MaxString+1)
	if err := ValidateString("long", long, lim); err == nil {
		t.Fatal("expected too long string to be rejected")
	}
}

func TestValidateString_NewlineAndTab(t *testing.T) {
	lim := DefaultLimits()
	lim.AllowNL = false
	lim.AllowTab = false
	if err := ValidateString("nl", "a\nb", lim); err == nil {
		t.Fatal("expected newline to be rejected when AllowNL is false")
	}
	if err := ValidateString("tab", "a\tb", lim); err == nil {
		t.Fatal("expected tab to be rejected when AllowTab is false")
	}
	lim.AllowNL = true
	lim.AllowTab = true
	if err := ValidateString("nl", "a\nb", lim); err != nil {
		t.Fatalf("newline should be allowed: %v", err)
	}
	if err := ValidateString("tab", "a\tb", lim); err != nil {
		t.Fatalf("tab should be allowed: %v", err)
	}
}

func TestValidatePath_Basics(t *testing.T) {
	lim := DefaultLimits()
	if err := ValidatePath("okpath", "/tmp/file.txt", lim); err != nil {
		t.Fatalf("valid path rejected: %v", err)
	}
	if err := ValidatePath("badpath", "a\x00b", lim); err == nil {
		t.Fatal("expected NUL in path to be rejected")
	}
}

func TestValidateStructStrings_SimpleStruct(t *testing.T) {
	type S struct {
		Name string
		Path string
	}
	lim := DefaultLimits()
	s := S{Name: "ok", Path: "/tmp/file"}
	if err := ValidateStructStrings(s, lim); err != nil {
		t.Fatalf("valid struct rejected: %v", err)
	}
	s.Name = "a\x00b"
	if err := ValidateStructStrings(s, lim); err == nil {
		t.Fatal("expected NUL in Name to be rejected")
	}
	s.Name = "ok"
	s.Path = "a\x00b"
	if err := ValidateStructStrings(s, lim); err == nil {
		t.Fatal("expected NUL in Path to be rejected")
	}
}

func TestValidateStructStrings_NestedStruct(t *testing.T) {
	type Inner struct {
		FilePath string
	}
	type Outer struct {
		Inner Inner
	}
	lim := DefaultLimits()
	o := Outer{Inner: Inner{FilePath: "/tmp/ok"}}
	if err := ValidateStructStrings(o, lim); err != nil {
		t.Fatalf("valid nested struct rejected: %v", err)
	}
	o.Inner.FilePath = "bad\x00path"
	if err := ValidateStructStrings(o, lim); err == nil {
		t.Fatal("expected NUL in nested FilePath to be rejected")
	}
}

func TestValidateStructStrings_SliceAndMap(t *testing.T) {
	type S struct {
		Names []string
		Paths map[string]string
	}
	lim := DefaultLimits()
	s := S{
		Names: []string{"ok", "fine"},
		Paths: map[string]string{"a": "/tmp/a", "b": "/tmp/b"},
	}
	if err := ValidateStructStrings(s, lim); err != nil {
		t.Fatalf("valid slice/map struct rejected: %v", err)
	}
	s.Names[1] = "bad\x00"
	if err := ValidateStructStrings(s, lim); err == nil {
		t.Fatal("expected NUL in slice to be rejected")
	}
	s.Names[1] = "fine"
	s.Paths["b"] = "bad\x00"
	if err := ValidateStructStrings(s, lim); err == nil {
		t.Fatal("expected NUL in map value to be rejected")
	}
}

// YAML/JSON decoded into interface{} nests values as map[string]interface{} and
// []interface{}, so every leaf string sits behind a reflect.Interface. Without an
// Interface case in walkValue the traversal stopped at the first such value and
// silently validated nothing — an oversized or NUL-bearing string slipped through.
// This mirrors how config.parseYAMLTemplate and Service.ValidateTemplate feed
// decoded template data in (&interface{}).
func TestValidateStructStrings_DecodedInterfaceTree(t *testing.T) {
	lim := DefaultLimits()
	doc := map[string]interface{}{
		"image": map[string]interface{}{
			"name":    "ok",
			"aliases": []interface{}{"a", "b"},
		},
	}
	if err := ValidateStructStrings(&doc, lim); err != nil {
		t.Fatalf("valid decoded tree rejected: %v", err)
	}

	// Oversized string behind two interface hops must be rejected.
	doc["image"].(map[string]interface{})["name"] = strings.Repeat("A", lim.MaxString+1)
	if err := ValidateStructStrings(&doc, lim); err == nil {
		t.Fatal("expected oversized string in decoded tree to be rejected")
	}

	// NUL inside an []interface{} element must be rejected too.
	doc["image"].(map[string]interface{})["name"] = "ok"
	doc["image"].(map[string]interface{})["aliases"] = []interface{}{"a", "bad\x00"}
	if err := ValidateStructStrings(&doc, lim); err == nil {
		t.Fatal("expected NUL in interface slice element to be rejected")
	}
}

func TestValidateStructStrings_PointerCycle(t *testing.T) {
	type Node struct {
		Value string
		Next  *Node
	}
	lim := DefaultLimits()
	n1 := &Node{Value: "ok"}
	n2 := &Node{Value: "ok", Next: n1}
	n1.Next = n2 // cycle
	if err := ValidateStructStrings(n1, lim); err != nil {
		t.Fatalf("cycle struct should not cause error: %v", err)
	}
	n2.Value = "bad\x00"
	if err := ValidateStructStrings(n1, lim); err == nil {
		t.Fatal("expected NUL in cycle struct to be rejected")
	}
}

func TestValidateFlagsAndArgs(t *testing.T) {
	cmd := &cobra.Command{
		Use: "test",
	}
	lim := DefaultLimits()
	cmd.Flags().String("name", "ok", "")
	cmd.Flags().String("file", "/tmp/ok", "")
	cmd.Flags().StringSlice("paths", []string{"/tmp/a", "/tmp/b"}, "")
	cmd.Flags().StringArray("names", []string{"foo", "bar"}, "")
	args := []string{"arg1", "arg2"}
	if err := validateFlagsAndArgs(cmd, args, lim); err != nil {
		t.Fatalf("valid flags/args rejected: %v", err)
	}
	if err := cmd.Flags().Set("name", "bad\x00"); err != nil {
		t.Fatalf("failed to set flag: %v", err)
	}
	if err := validateFlagsAndArgs(cmd, args, lim); err == nil {
		t.Fatal("expected NUL in flag to be rejected")
	}
	if err := cmd.Flags().Set("name", "ok"); err != nil {
		t.Fatalf("failed to set flag: %v", err)
	}
	if err := cmd.Flags().Set("file", "bad\x00"); err != nil {
		t.Fatalf("failed to set flag: %v", err)
	}
	if err := validateFlagsAndArgs(cmd, args, lim); err == nil {
		t.Fatal("expected NUL in file flag to be rejected")
	}
	if err := cmd.Flags().Set("file", "/tmp/ok"); err != nil {
		t.Fatalf("failed to set flag: %v", err)
	}
	if err := cmd.Flags().Set("paths", "/tmp/a,bad\x00"); err != nil {
		t.Fatalf("failed to set flag: %v", err)
	}
	if err := validateFlagsAndArgs(cmd, args, lim); err == nil {
		t.Fatal("expected NUL in stringSlice flag to be rejected")
	}
	if err := cmd.Flags().Set("paths", "/tmp/a,/tmp/b"); err != nil {
		t.Fatalf("failed to set flag: %v", err)
	}
	if err := cmd.Flags().Set("names", "foo"); err != nil {
		t.Fatalf("failed to set flag: %v", err)
	}
	if err := cmd.Flags().Set("names", "bad\x00"); err != nil {
		t.Fatalf("failed to set flag: %v", err)
	}
	if err := validateFlagsAndArgs(cmd, args, lim); err == nil {
		t.Fatal("expected NUL in stringArray flag to be rejected")
	}
}

func TestAttachRecursive_PersistentPreRunE(t *testing.T) {
	root := &cobra.Command{Use: "root"}
	child := &cobra.Command{Use: "child"}
	root.AddCommand(child)
	lim := DefaultLimits()
	AttachRecursive(root, lim)
	root.Flags().String("name", "ok", "")
	child.Flags().String("name", "ok", "")
	// Should not error
	if err := root.PersistentPreRunE(root, []string{"ok"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := child.Flags().Set("name", "bad\x00"); err != nil {
		t.Fatalf("failed to set flag: %v", err)
	}
	if err := child.PersistentPreRunE(child, []string{"ok"}); err == nil {
		t.Fatal("expected error for bad flag in child")
	}
}
