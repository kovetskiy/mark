// Package chrome centralises the headless Chrome configuration shared by the
// diagram renderers. Both the d2 and mermaid renderers drive Chrome through
// chromedp, and they previously configured their allocators independently,
// which let the two launch paths drift apart.
package chrome

import "github.com/chromedp/chromedp"

// AllocatorOptions returns the options mark appends to
// chromedp.DefaultExecAllocatorOptions.
//
// Only genuinely additive options belong here. The chromedp defaults already
// set headless, no-first-run, no-default-browser-check and
// disable-dev-shm-usage, so repeating them would be noise.
//
// The Chrome sandbox is disabled unconditionally. Chrome refuses to start with
// "No usable sandbox!" wherever unprivileged user namespaces are unavailable --
// restricted containers, Kubernetes pods with a hardened seccomp profile, and
// Ubuntu 23.10+ with its AppArmor userns restrictions -- and mark has no
// reliable way to detect that before launching the browser. Note that chromedp
// already passes --no-sandbox on its own when running as root (uid 0), so this
// only changes behaviour for non-root runs.
func AllocatorOptions() []chromedp.ExecAllocatorOption {
	return []chromedp.ExecAllocatorOption{
		chromedp.DisableGPU,
		chromedp.NoSandbox,
		chromedp.Flag("disable-setuid-sandbox", true),
	}
}
