package oepinstaller

import (
	"fmt"
	"slices"
	"strings"
)

// IsKernelImage reports whether name is a kernel image package, such as
// "linux-image-7.0.0-38-generic".
func IsKernelImage(name string) bool {
	return classifyPackage(name).KernelRole() == KernelImage
}

// CheckKernel checks the kernel entries of out against the kernel packages
// the resolved image installs (exact package names). A kernel image must be
// one of them; a meta-package must name the series of one of their releases;
// headers, modules, and tools must be for one of their releases.
func CheckKernel(out *Output, kernelPackages []string) error {
	var names, releases []string
	for _, k := range kernelPackages {
		name := packageName(k)
		names = append(names, name)
		if p := classifyPackage(name); p.KernelRole() == KernelImage {
			releases = append(releases, p.KernelRelease())
		}
	}
	var problems []string
	for _, p := range out.Packages {
		if problem := kernelProblem(p, names, releases); problem != "" {
			problems = append(problems, problem)
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("the OEP Installer modules need a different kernel than the template installs %v: %s",
			kernelPackages, strings.Join(problems, "; "))
	}
	return nil
}

// kernelProblem describes why p does not match the template's kernel, or
// returns "" when it does or is not a kernel entry.
func kernelProblem(p Package, names, releases []string) string {
	switch p.KernelRole() {
	case KernelImage:
		if !slices.Contains(names, p.Name) {
			return fmt.Sprintf("%s is not a kernel package of the template", p.Entry)
		}
	case KernelMeta:
		series := p.KernelSeries()
		if !slices.ContainsFunc(releases, func(r string) bool { return inSeries(r, series) }) {
			return fmt.Sprintf("%s needs a %s kernel", p.Entry, seriesOrAny(series))
		}
	case KernelExtra:
		if !slices.Contains(releases, p.KernelRelease()) {
			return fmt.Sprintf("%s is for kernel %s", p.Entry, p.KernelRelease())
		}
	}
	return ""
}

// inSeries reports whether kernel release r belongs to series, such as "6.18"
// for "6.18.2-1-generic" or "6.18-intel"; an empty series matches any release.
func inSeries(r, series string) bool {
	return series == "" || strings.HasPrefix(r, series+".") || strings.HasPrefix(r, series+"-")
}

func seriesOrAny(series string) string {
	if series == "" {
		return "matching"
	}
	return series + ".x"
}
