// Package chrome centralises the headless Chrome configuration shared by the
// diagram renderers. Both the d2 and mermaid renderers drive Chrome through
// chromedp, and they previously configured their allocators independently,
// which let the two launch paths drift apart.
package chrome

import (
	"time"

	"github.com/chromedp/chromedp"
)

// wsURLReadTimeout is how long to wait for Chrome to print the DevTools
// websocket URL it is reached on.
//
// chromedp defaults this to 20 seconds, which is generous for an idle desktop
// and too tight for a loaded machine. Chrome has already been spawned by the
// time this clock starts, so what is being waited on is the browser getting far
// enough through startup to write one line -- exactly the thing that stretches
// when CPU is contended. Overshooting it fails the run with "websocket url
// timeout reached", which reads like a Chrome or network fault rather than a
// machine that was merely busy.
//
// mark hits this in the places it is least able to afford: CI containers with a
// fraction of a core, and its own test suite, where `go test ./...` runs the d2,
// mermaid and markdown packages in parallel and each launches its own browser.
//
// Raising it costs nothing when Chrome starts promptly, and nothing when Chrome
// is absent or unrunnable -- that fails at exec time, before this wait. The only
// case that pays is a browser that starts but never reports, where the choice is
// between failing at 20 seconds and failing at 60.
const wsURLReadTimeout = 60 * time.Second

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
		chromedp.WSURLReadTimeout(wsURLReadTimeout),
	}
}
