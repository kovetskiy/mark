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
// A link that names a server of its own is refused unless it is the
// configured one, scheme and port included: the credentials go wherever the
// download does.
func (api *API) AttachmentURL(pageID string, info AttachmentInfo) (string, error) {
	if api.gateway && info.ID != "" {
		return strings.TrimSuffix(api.BaseURL, "/") + "/rest/api/content/" + url.PathEscape(pageID) +
			"/child/attachment/" + url.PathEscape(info.ID) + "/download", nil
	}

	link := info.Links.Download
	if link == "" {
		return "", fmt.Errorf("attachment %q has no download link", info.Filename)
	}

	base, err := url.Parse(api.BaseURL)
	if err != nil {
		return "", fmt.Errorf("unable to parse base URL %q: %w", api.BaseURL, err)
	}

	target, err := url.Parse(link)
	if err != nil {
		return "", fmt.Errorf("unable to parse download link %q of attachment %q: %w", link, info.Filename, err)
	}

	if target.IsAbs() {
		if !sameServer(target, base) {
			return "", fmt.Errorf(
				"attachment %q is served from %s://%s rather than from %s://%s; refusing to send credentials there",
				info.Filename, target.Scheme, target.Host, base.Scheme, base.Host,
			)
		}

		return target.String(), nil
	}

	prefix := strings.TrimSuffix(api.BaseURL, "/")
	if context := strings.TrimSuffix(info.Links.Context, "/"); context != "" && !strings.HasSuffix(prefix, context) {
		prefix += context
	}

	if !strings.HasPrefix(link, "/") {
		link = "/" + link
	}

	return prefix + link, nil
}

// sameServer says whether a and b name the same scheme, host and port, with a
// port left out read as the scheme's default.
func sameServer(a, b *url.URL) bool {
	port := func(u *url.URL) string {
		if p := u.Port(); p != "" {
			return p
		}
		switch strings.ToLower(u.Scheme) {
		case "http":
			return "80"
		case "https":
			return "443"
		}
		return ""
	}

	return strings.EqualFold(a.Scheme, b.Scheme) &&
		strings.EqualFold(a.Hostname(), b.Hostname()) &&
		port(a) == port(b)
}

// DownloadAttachment writes the content of an attachment of page pageID to w.
func (api *API) DownloadAttachment(ctx context.Context, pageID string, info AttachmentInfo, w io.Writer) error {
	address, err := api.AttachmentURL(pageID, info)
	if err != nil {
		return err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return fmt.Errorf("unable to build the download of attachment %q: %w", info.Filename, err)
	}

	api.v1.authorize(request)

	response, err := api.httpClient().Do(request)
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

	written, err := io.Copy(w, io.LimitReader(response.Body, maxAttachmentDownload+1))
	if err != nil {
		return fmt.Errorf("unable to download attachment %q: %w", info.Filename, err)
	}

	if written > maxAttachmentDownload {
		return fmt.Errorf("attachment %q is larger than %d bytes", info.Filename, maxAttachmentDownload)
	}

	return nil
}

// httpClient is the client every request of the run goes through, with its
// retries, timeouts and TLS settings.
func (api *API) httpClient() *http.Client {
	if api.v1 != nil && api.v1.http != nil {
		return api.v1.http
	}

	return http.DefaultClient
}
