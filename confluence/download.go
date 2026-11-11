package confluence

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// maxAttachmentDownload bounds what DownloadAttachment writes. It is well
// above what Confluence accepts as an upload by default (100 MB), so a real
// attachment is never cut short, while a server answering with an endless body
// cannot fill the disk.
const maxAttachmentDownload = 1 << 30

// ContentProperties returns every property stored against a page or blogpost:
// through v1 where it can, and through v2 on the scoped-token gateway, which
// refuses v1.
func (api *API) ContentProperties(ctx context.Context, contentID string) ([]Property, error) {
	if !api.gateway {
		return api.ListContentProperties(ctx, contentID)
	}

	collection, err := api.collectionOfV2(ctx, contentID)
	if err != nil {
		return nil, fmt.Errorf("unable to read properties of content %s: %w", contentID, err)
	}

	return api.listPropertiesV2(ctx, collection, contentID)
}

// AttachmentURL is where the content of an attachment of page pageID is
// downloaded from.
//
// Through the api.atlassian.com gateway that is the REST download endpoint:
// the gateway proxies the API, not the site's download servlet. Elsewhere it
// is the attachment's download link, which is relative to the instance's
// context -- "/wiki" on Cloud -- and a base URL configured the documented way
// already ends with that context.
//
// A download that would go to a server other than the configured one, scheme
// and port included, is refused, whether the link names that server itself or
// its context path changes the host it is joined to: the credentials go
// wherever the download does.
func (api *API) AttachmentURL(pageID string, info AttachmentInfo) (string, error) {
	if api.gateway && info.ID != "" {
		return strings.TrimSuffix(api.baseURL, "/") + "/rest/api/content/" + url.PathEscape(pageID) +
			"/child/attachment/" + url.PathEscape(info.ID) + "/download", nil
	}

	link := info.Links.Download
	if link == "" {
		return "", fmt.Errorf("attachment %q has no download link", info.Filename)
	}

	base, err := url.Parse(api.baseURL)
	if err != nil {
		return "", fmt.Errorf("unable to parse base URL %q: %w", api.baseURL, err)
	}

	target, err := url.Parse(link)
	if err != nil {
		return "", fmt.Errorf("unable to parse download link %q of attachment %q: %w", link, info.Filename, err)
	}

	address := target.String()
	if !target.IsAbs() {
		prefix := strings.TrimSuffix(api.baseURL, "/")
		if context := strings.TrimSuffix(info.Links.Context, "/"); context != "" && !strings.HasSuffix(prefix, context) {
			prefix += context
		}

		if !strings.HasPrefix(link, "/") {
			link = "/" + link
		}

		address = prefix + link
	}

	// The context path is pasted onto the base URL as given, so after a base
	// with no path "@x" or ".x" names another host: check what was joined.
	final, err := url.Parse(address)
	if err != nil {
		return "", fmt.Errorf("unable to parse download link %q of attachment %q: %w", address, info.Filename, err)
	}

	if !sameServer(final, base) {
		return "", fmt.Errorf(
			"attachment %q is served from %s://%s rather than from %s://%s; refusing to send credentials there",
			info.Filename, final.Scheme, final.Host, base.Scheme, base.Host,
		)
	}

	return address, nil
}

// sameServer says whether link reaches the server at base, by the scheme,
// host and port SameInstance compares. A link that is not one it compares,
// such as one carrying a user name, is no server of the configured one; a user
// name in base itself was given by whoever configured it, so it is ignored,
// where SameInstance refuses a user name in either of its arguments.
func sameServer(link, base *url.URL) bool {
	server := *base
	server.User = nil
	x, errA := originOf(link)
	y, errB := originOf(&server)
	return errA == nil && errB == nil && x == y
}

// ErrAttachmentSizeMismatch reports that a download was not the size
// Confluence records for the attachment: something other than Confluence
// answered for it, or it changed between being listed and being downloaded.
var ErrAttachmentSizeMismatch = errors.New("the download is not the size Confluence records for the attachment")

// DownloadAttachment writes the content of an attachment of page pageID to w.
//
// An answer that is a login page rather than the attachment is an error
// errors.Is finds ErrLoginPage in, and nothing is written. That is a redirect
// to Seraph's /login.action, Seraph saying it did not authenticate the
// request, or -- on any host, so an identity provider's form too -- an HTML
// answer for an attachment info records as something other than HTML, or one
// that is not the size info records.
//
// Any other answer that is not the size info records is an error errors.Is
// finds ErrAttachmentSizeMismatch in. When the answer gives its length that
// is known before anything is written; when it does not, w has been given the
// whole body by the time the error is returned, and discarding it is up to
// the caller.
//
// info is the attachment as GetAttachments lists it. Neither check is made
// when it records no media type or no size.
func (api *API) DownloadAttachment(ctx context.Context, pageID string, info AttachmentInfo, w io.Writer) error {
	address, err := api.AttachmentURL(pageID, info)
	if err != nil {
		return err
	}

	if api.v1 == nil {
		return errNoClient
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return fmt.Errorf("unable to build the download of attachment %q: %w", info.Filename, err)
	}

	api.v1.authorize(request)

	response, err := api.v1.http.Do(request)
	if err != nil {
		return fmt.Errorf("unable to download attachment %q: %w", info.Filename, err)
	}
	defer func() {
		_ = response.Body.Close()
	}()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf(
			"unable to download attachment %q: %s answered %s",
			info.Filename, address, response.Status,
		)
	}

	mediaType := contentMediaType(response.Header)
	evidence, strong := loginEvidence(request, response, mediaType, true)
	if evidence == "" {
		evidence, strong = recordEvidence(info, mediaType), true
	}
	if evidence != "" {
		return downloadLoginError(info, request, response, evidence, strong)
	}

	size, sized := recordedSize(info, response)
	if sized && response.ContentLength >= 0 && response.ContentLength != size {
		return sizeMismatch(info, request, response, mediaType, response.ContentLength, size)
	}

	written, err := io.Copy(w, io.LimitReader(response.Body, maxAttachmentDownload+1))
	if err != nil {
		return fmt.Errorf("unable to download attachment %q: %w", info.Filename, err)
	}

	if written > maxAttachmentDownload {
		return fmt.Errorf("attachment %q is larger than %d bytes", info.Filename, maxAttachmentDownload)
	}

	if sized && written != size {
		return sizeMismatch(info, request, response, mediaType, written, size)
	}

	return nil
}

// recordEvidence says why an answer of mediaType is a login page rather than
// the attachment info describes, or "": Confluence serves an attachment as
// the type it records for it, so HTML for one it does not record as HTML was
// put there by something else.
func recordEvidence(info AttachmentInfo, mediaType string) string {
	recorded := info.recordedMediaType()
	if recorded == "" || isHTMLMediaType(recorded) || !isHTMLMediaType(mediaType) {
		return ""
	}

	return "an HTML Content-Type for an attachment Confluence records as " + recorded
}

// recordedSize is the size info records, and whether the answer can be held
// to it: a body that came encoded and was not decoded on the way is not the
// file byte for byte. One net/http decoded is, and has no Content-Length.
func recordedSize(info AttachmentInfo, response *http.Response) (int64, bool) {
	if info.Extensions.FileSize == nil {
		return 0, false
	}

	if encoding := response.Header.Get("Content-Encoding"); !response.Uncompressed &&
		encoding != "" && !strings.EqualFold(encoding, "identity") {
		return 0, false
	}

	return *info.Extensions.FileSize, true
}

// sizeMismatch is the error for an answer of answered bytes to the download
// of an attachment Confluence records as recorded bytes. One in HTML is a
// login page; one in anything else may be the attachment changed since it was
// listed, or a proxy cutting it short, and no login was asked for.
func sizeMismatch(
	info AttachmentInfo, request *http.Request, response *http.Response, mediaType string, answered, recorded int64,
) error {
	if isHTMLMediaType(mediaType) {
		return downloadLoginError(info, request, response, fmt.Sprintf(
			"an HTML answer of %d bytes for an attachment Confluence records as %d", answered, recorded,
		), true)
	}

	return fmt.Errorf(
		"unable to download attachment %q: %w: %s answered with %d bytes where Confluence records %d; "+
			"it may have changed since it was listed, or something in front of Confluence answered for it",
		info.Filename, ErrAttachmentSizeMismatch, request.URL.Redacted(), answered, recorded,
	)
}

// downloadLoginError is the error for a download answered with a login page.
func downloadLoginError(
	info AttachmentInfo, request *http.Request, response *http.Response, evidence string, strong bool,
) error {
	return requestError(fmt.Sprintf("download attachment %q", info.Filename), &loginPageError{
		answer: &reply{
			StatusCode: response.StatusCode, Status: response.Status, Request: response.Request,
		},
		requested: request.URL.Redacted(), evidence: evidence, strong: strong,
	})
}
