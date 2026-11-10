package confluence

import (
	"context"
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

// DownloadAttachment writes the content of an attachment of page pageID to w.
// An answer that is a login page rather than the attachment -- a redirect to
// Seraph's /login.action, or Seraph saying it did not authenticate the
// request -- is an error errors.Is finds ErrLoginPage in, and nothing is
// written.
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

	if evidence, strong := loginEvidence(request, response, contentMediaType(response.Header), true); evidence != "" {
		return requestError(fmt.Sprintf("download attachment %q", info.Filename), &loginPageError{
			answer: &reply{
				StatusCode: response.StatusCode, Status: response.Status, Request: response.Request,
			},
			requested: request.URL.Redacted(), evidence: evidence, strong: strong,
		})
	}

	written, err := io.Copy(w, io.LimitReader(response.Body, maxAttachmentDownload+1))
	if err != nil {
		return fmt.Errorf("unable to download attachment %q: %w", info.Filename, err)
	}

	if written > maxAttachmentDownload {
		return fmt.Errorf("attachment %q is larger than %d bytes", info.Filename, maxAttachmentDownload)
	}

	return nil
}
