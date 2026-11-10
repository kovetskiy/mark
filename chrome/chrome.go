// Package chrome centralises the headless Chrome configuration shared by the
// diagram and formula renderers. The d2, mermaid and math renderers all drive
// Chrome through chromedp -- d2 and math through PNGFromSVG, mermaid through
// its own engine started with AllocatorOptions -- and they previously
// configured their allocators independently, which let the launch paths drift
// apart.
package chrome

import (
	"errors"

	"github.com/chromedp/chromedp"
)

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
//
// The browser is also kept off the network. A picture is drawn from the
// document alone -- mermaid.js comes with mark, and every page and drawing is
// handed over as a data: URL -- so an address a diagram or formula names is
// not fetched by the browser, the same as d2's SVG output, which fetches
// nothing unless --d2-bundle-remote says so.
func AllocatorOptions() []chromedp.ExecAllocatorOption {
	return []chromedp.ExecAllocatorOption{
		chromedp.DisableGPU,
		chromedp.NoSandbox,
		chromedp.Flag("disable-setuid-sandbox", true),

		// Two ways of saying the same thing, so that neither rests on how one
		// Chrome release reads its flag: no host, IP literals and localhost
		// included, resolves to an address; and every request goes to a proxy
		// that does not exist (".invalid" never resolves). "<-loopback>" takes
		// back Chrome's own bypass of the proxy for localhost and 127.0.0.1.
		chromedp.Flag("host-resolver-rules", "MAP * ~NOTFOUND"),
		chromedp.ProxyServer("http://offline.invalid:9"),
		chromedp.Flag("proxy-bypass-list", "<-loopback>"),

		// WebRTC can send UDP around a proxy; keep it to the proxy as well.
		chromedp.Flag("force-webrtc-ip-handling-policy", "disable_non_proxied_udp"),
	}
}

// ErrNetworkUnavailable is what a picture that needs something from the
// network fails with, through errors.Is, where mark can tell.
var ErrNetworkUnavailable = errors.New("diagrams and formulas are drawn without network access")
