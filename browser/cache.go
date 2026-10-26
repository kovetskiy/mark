// Package browser authenticates mark against Confluence by letting the user
// log in through a real browser and reusing the session cookie that results.
//
// It is three separable pieces: a cache that remembers cookies between runs, a
// probe that decides whether a set of cookies actually authenticates, and the
// browser driving that produces them in the first place. Only the last needs
// Chrome, which is what keeps the rest testable.
package browser

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/rs/zerolog/log"
)

// storedCookie is a cookie as it is written to disk. http.Cookie is not used
// directly: it carries unexported state and fields that mean nothing once the
// response is gone, and pinning the wire format here keeps a Go standard
// library change from silently altering the file.
type storedCookie struct {
	Name     string    `json:"name"`
	Value    string    `json:"value"`
	Domain   string    `json:"domain,omitempty"`
	Path     string    `json:"path,omitempty"`
	Expires  time.Time `json:"expires,omitempty"`
	Secure   bool      `json:"secure,omitempty"`
	HTTPOnly bool      `json:"httpOnly,omitempty"`
}

// cacheFile is the whole file: cookies keyed by host, so one file serves
// several Confluence instances.
type cacheFile struct {
	Hosts map[string][]storedCookie `json:"hosts"`
}

// CachePath is where the cookies are kept.
func CachePath() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("unable to locate the user cache directory: %w", err)
	}

	return filepath.Join(dir, "mark", "cookies.json"), nil
}

// hostKey is the cache key for an instance. Cookies belong to a host, and the
// path a Confluence lives under -- /wiki on Cloud -- does not change that.
func hostKey(baseURL string) (string, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("unable to parse %q as url: %w", baseURL, err)
	}

	if parsed.Host == "" {
		return "", fmt.Errorf("no host in %q", baseURL)
	}

	return parsed.Host, nil
}

// LoadCookies returns the cached cookies for baseURL, or nil when there are
// none to be had.
//
// Every failure -- no file, no cache directory, unreadable, corrupt -- returns
// nil rather than an error. The cache is an optimisation, and the fallback is
// to log in again; a tool that cannot publish because its cache is malformed
// would be worse than one that opens a browser.
func LoadCookies(baseURL string) []*http.Cookie {
	path, err := CachePath()
	if err != nil {
		log.Debug().Err(err).Msg("no cookie cache available")
		return nil
	}

	host, err := hostKey(baseURL)
	if err != nil {
		log.Debug().Err(err).Msg("no cookie cache available")
		return nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			log.Warn().Err(err).Msgf("unable to read the cookie cache at %s; logging in again", path)
		}
		return nil
	}

	var file cacheFile
	if err := json.Unmarshal(data, &file); err != nil {
		log.Warn().Err(err).Msgf("the cookie cache at %s is not readable json; logging in again", path)
		return nil
	}

	now := time.Now()

	var cookies []*http.Cookie
	for _, stored := range file.Hosts[host] {
		// A zero expiry is a session cookie, which is exactly what a browser
		// login produces: it has no expiry to check, only a server that will
		// eventually stop honouring it.
		if !stored.Expires.IsZero() && stored.Expires.Before(now) {
			continue
		}

		// The attributes are whatever the browser issued and the cache
		// recorded, not ours to invent: forcing Secure on would break a
		// self-hosted Confluence Server reached over plain HTTP, and SameSite
		// is a server directive with no meaning on a request cookie.
		cookies = append(cookies, &http.Cookie{ //nolint:gosec // G124: reconstructing a captured cookie, not issuing one
			Name:     stored.Name,
			Value:    stored.Value,
			Domain:   stored.Domain,
			Path:     stored.Path,
			Expires:  stored.Expires,
			Secure:   stored.Secure,
			HttpOnly: stored.HTTPOnly,
		})
	}

	return cookies
}

// SaveCookies records the cookies for baseURL, leaving other hosts' entries
// alone.
func SaveCookies(baseURL string, cookies []*http.Cookie) error {
	path, err := CachePath()
	if err != nil {
		return err
	}

	host, err := hostKey(baseURL)
	if err != nil {
		return err
	}

	file := cacheFile{Hosts: map[string][]storedCookie{}}

	// Read before write so that saving one instance's cookies does not evict
	// another's. A file that cannot be read is replaced rather than merged
	// into -- it was already unusable.
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &file); err != nil || file.Hosts == nil {
			file = cacheFile{Hosts: map[string][]storedCookie{}}
		}
	}

	stored := make([]storedCookie, 0, len(cookies))
	for _, cookie := range cookies {
		stored = append(stored, storedCookie{
			Name:     cookie.Name,
			Value:    cookie.Value,
			Domain:   cookie.Domain,
			Path:     cookie.Path,
			Expires:  cookie.Expires,
			Secure:   cookie.Secure,
			HTTPOnly: cookie.HttpOnly,
		})
	}

	file.Hosts[host] = stored

	data, err := json.Marshal(file)
	if err != nil {
		return fmt.Errorf("unable to encode the cookie cache: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("unable to create the cookie cache directory: %w", err)
	}

	// Written through a temporary file in the same directory so that an
	// interrupted write leaves the previous cache intact rather than a
	// half-written one. The mode is set on the temporary file, before it holds
	// any cookie, so the credential is never briefly world-readable.
	temp, err := os.CreateTemp(filepath.Dir(path), "cookies-*.json")
	if err != nil {
		return fmt.Errorf("unable to write the cookie cache: %w", err)
	}
	defer os.Remove(temp.Name())

	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return fmt.Errorf("unable to secure the cookie cache: %w", err)
	}

	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return fmt.Errorf("unable to write the cookie cache: %w", err)
	}

	if err := temp.Close(); err != nil {
		return fmt.Errorf("unable to write the cookie cache: %w", err)
	}

	if err := os.Rename(temp.Name(), path); err != nil {
		return fmt.Errorf("unable to write the cookie cache: %w", err)
	}

	return nil
}
