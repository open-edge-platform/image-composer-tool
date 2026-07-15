# ADR: Dependency Graph Analyzer Tool

**Status**: Proposed  
**Date**: 2026-01-08  
**Updated**: N/A  
**Authors**: ICT Team  
**Technical Area**: Dependency Analysis / Graph Visualization

---

## Summary

This ADR proposes adding a **dependency graph analyzer** tool (`dep-analyzer`)
to the ICT.

The tool enables users to:
- Slice DOT dependency graphs by root package and traversal depth
- Perform reverse dependency analysis ("what depends on package X?")
- Query graph statistics and identify root/leaf packages
- Export subgraphs in multiple formats (DOT, SVG, PNG, PDF)

The goal is to make dependency analysis **fast, intuitive, and automatable**,
without requiring users to learn gvpr or write custom graph programs.

---

## Context

### Problem Statement

The ICT generates DOT format dependency graphs via the `--dotfile`
flag. These graphs visualize package dependencies for Linux OS images and can
contain hundreds of packages with thousands of dependency edges.

Users often need to understand or debug these dependency graphs. Common questions
include:

- What are the direct dependencies of package X?
- What are the transitive dependencies up to N levels deep?
- Which packages depend on package X (reverse dependencies)?
- Which packages are top-level (nothing depends on them)?
- Which packages are leaf nodes (have no dependencies)?
- How can I export a focused subgraph for documentation?

Today, answering these questions requires:
- Manually inspecting large DOT files (impractical for 200+ packages)
- Writing custom gvpr programs (requires specialized knowledge)
- Using heavyweight GUI tools like Gephi (complex import/export workflow)

This approach presents several challenges:

- High cognitive load and poor user experience
- gvpr syntax is non-obvious and error-prone
- Output is difficult to automate or integrate into CI/CD
- No consistent way to extract focused subgraphs

---

### Background

Users may want to:
- Debug "why is package X included in my image?"
- Perform impact analysis before removing a package
- Generate focused dependency diagrams for documentation
- Validate dependency chains in automated pipelines

This tool is intended to complement the `--dotfile` feature by providing
analysis capabilities for the generated graphs.

---

## Decision / Recommendation

We will introduce a dedicated **dep-analyzer** utility as a shell script in
the `scripts/` directory.

The implementation uses **graphviz's gvpr** for graph processing to ensure
consistent behavior and avoid additional dependencies beyond graphviz (which
is already required for DOT rendering).

---

## Core Design Principles

1. **Simple CLI Interface**  
   Provide clear, discoverable options that map to common analysis tasks.

2. **Separation of Concerns**  
   - Slicing logic uses BFS traversal with configurable depth
   - Query modes (list-roots, list-leaves, stats) operate independently
   - Rendering is decoupled from graph processing

3. **Format Flexibility**  
   - Multiple output formats supported: DOT, SVG, PNG, PDF
   - Auto-generated filenames reflect analysis parameters

---

## Command Line Interface

The tool will be invoked as a standalone script:

```bash
# Slice dependencies of a package (forward traversal)
dep-analyzer.sh -i deps.dot -r vim -d 2

# Reverse mode - who depends on this package?
dep-analyzer.sh -i deps.dot -r libc6 -d 2 --reverse

# Render to SVG format
dep-analyzer.sh -i deps.dot -r systemd -d 3 -t svg

# List all top-level packages (no incoming edges)
dep-analyzer.sh -i deps.dot --list-roots

# List all base packages (no outgoing edges)
dep-analyzer.sh -i deps.dot --list-leaves

# Show graph statistics
dep-analyzer.sh -i deps.dot --stats
```

### CLI Options

| Option | Description |
|--------|-------------|
| `-i, --input FILE` | Input DOT file (required) |
| `-r, --root NAME` | Root package name for slicing |
| `-d, --depth N` | Maximum traversal depth (default: 2) |
| `-o, --output FILE` | Output file (auto-generated if omitted) |
| `-t, --type TYPE` | Output format: dot, svg, png, pdf (default: dot) |
| `--reverse` | Reverse edge direction for "who depends on X" queries |
| `--highlight-root` | Visually highlight the root node |
| `--list-roots` | List packages with no incoming edges |
| `--list-leaves` | List packages with no outgoing edges |
| `--stats` | Display graph statistics |
| `-h, --help` | Show usage information |

---

## Output Formats

The tool supports multiple output formats via the `-t/--type` option:

| Format | Use Case |
|--------|----------|
| `dot` | Further processing, input to other tools |
| `svg` | Documentation, web embedding (scalable) |
| `png` | Presentations, image embedding |
| `pdf` | Reports, print-ready documents |

---

## Auto-generated Filenames

When `-o` is not specified, filenames are generated based on analysis parameters:

```
<input-stem>_<root>[_d<depth>][_reverse].<type>
```

Examples:
- `deps_vim_d2.svg` - vim dependencies, depth 2, SVG format
- `deps_libc6_d3_reverse.svg` - packages depending on libc6, depth 3
- `deps_apt.dot` - apt dependencies, default depth, DOT format

---

## Analysis Modes

### Slicing Mode

Extracts a subgraph using BFS traversal from a root package:

- **Forward traversal** (default): Follow outgoing edges (dependencies of root)
- **Reverse traversal** (`--reverse`): Follow incoming edges (dependents of root)
- **Depth limit**: Controls how many hops from root to include

### Query Mode

Provides quick answers without generating graph output:

- **`--list-roots`**: Packages with no incoming edges (top-level packages)
- **`--list-leaves`**: Packages with no outgoing edges (base packages)
- **`--stats`**: Node count, edge count, root count, leaf count

---

## Consequences and Trade-offs

**Pros**

- Significantly improved UX for dependency analysis
- Consistent, automatable output
- No additional dependencies beyond graphviz
- Complements the `--dotfile` feature with analysis capabilities
- Low maintenance overhead (~200 lines of bash)

**Cons**

- Shell script requires graphviz to be installed
- Limited to DOT format input (by design, matches image-composer-tool output)
- BFS traversal may not suit all analysis patterns

---

## Alternatives Considered

### Extend image-composer-tool with built-in analysis

**Rejected** — Would add complexity to the main tool and require Go
implementation. A separate utility keeps concerns separated and is faster
to iterate on.

### Python-based tool using networkx

**Rejected** — Adds Python dependency. gvpr is already available with graphviz
and handles DOT natively without additional installation.

### Interactive web-based viewer (d3-graphviz)

**Rejected** — Requires web server setup, more complex for CLI-focused
workflows. Could be a future addition for interactive exploration.

### Recommend external tools (Gephi, yEd)

**Rejected** — These are GUI-heavy, require manual import/export, and don't
understand our semantic colors.

---

## Non-Goals

- Modifying the original DOT file
- Generating DOT files (that's image-composer-tool's job)
- Real-time or interactive analysis
- Supporting non-DOT graph formats

---

## Dependencies

- **graphviz** package (provides `gvpr`, `dot` commands)
- Bash 4.0+ (for standard shell features)

---

## Error Handling

The tool handles various error conditions gracefully:

| Error Condition | Behavior |
|-----------------|----------|
| Input file not found | Return error with clear message |
| Root package not in graph | Return error with suggestion to verify name |
| Invalid depth value | Return error indicating valid range |
| graphviz not installed | Return error with installation hint |
| Unsupported output type | Return error listing supported formats |

---

## References

- [Graphviz gvpr documentation](https://graphviz.org/pdf/gvpr.1.pdf)
- [DOT language specification](https://graphviz.org/doc/info/lang.html)
- image-composer-tool `--dotfile` flag documentation
