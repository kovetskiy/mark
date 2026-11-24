package mark

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/kovetskiy/mark/v17/confluence"
	"github.com/kovetskiy/mark/v17/fingerprint"
	"github.com/kovetskiy/mark/v17/internal/logctx"
)

// statusChange plans what to do with the status after page settings are written.
type statusChange struct {
	current, desired *confluence.PageContentStatus
	declared         bool
}

// validateContentStatusPlatform rejects declarations for unsupported targets
// before resolving a destination can create a page.
func (r *publishRun) validateContentStatusPlatform(ctx context.Context, pageType confluence.ContentType) error {
	if pageType != confluence.ContentBlogPost {
		cloud, err := r.api.Cloud(ctx)
		if err != nil || cloud {
			return err
		}
	}
	return errors.New("content status is supported only for Confluence Cloud pages")
}

// contentStatusPlan reads the status to preserve or resolves the one requested.
// The zero change leaves a page with no status alone.
func (r *publishRun) contentStatusPlan(ctx context.Context, doc *document, target *confluence.PageInfo, preserve bool) (statusChange, error) {
	var name *string
	if doc.meta != nil {
		name = doc.meta.ContentStatus
	}
	if name == nil && !preserve {
		return statusChange{}, nil
	}
	if target.Type == confluence.ContentBlogPost {
		if name != nil {
			return statusChange{}, errors.New("content status is supported only for Confluence Cloud pages")
		}
		return statusChange{}, nil
	}
	cloud, err := r.api.Cloud(ctx)
	if err != nil {
		if name == nil && !confluence.IsCancellation(err) {
			return statusChange{}, nil
		}
		return statusChange{}, err
	}
	if !cloud {
		if name != nil {
			return statusChange{}, errors.New("content status is supported only for Confluence Cloud pages")
		}
		return statusChange{}, nil
	}

	current, err := r.api.ReadPageContentStatus(ctx, target.ID)
	if err != nil {
		if name != nil || confluence.IsCancellation(err) {
			return statusChange{}, err
		}
		if !statusReadUnavailable(err) {
			logctx.From(ctx).Warn().Err(err).Msgf("unable to read content status to preserve on page %s", target.ID)
		}
		return statusChange{}, nil
	}
	change := statusChange{current: current, desired: current, declared: name != nil}
	if name != nil {
		change.desired = nil
		if *name != "" {
			change.desired, err = r.api.ResolvePageContentStatus(ctx, target.ID, *name)
		}
	}
	return change, err
}

// statusReadUnavailable reports that the optional read cannot be made with
// this endpoint or token. An explicit status declaration still needs access.
func statusReadUnavailable(err error) bool {
	return errors.Is(err, confluence.ErrNotFound) ||
		errors.Is(err, confluence.ErrUnauthorized) || errors.Is(err, confluence.ErrForbidden)
}

// differs reports whether the planned status differs from the observed status.
func (change statusChange) differs() bool {
	if change.current == nil || change.desired == nil {
		return change.current != change.desired
	}
	return change.current.ID != change.desired.ID
}

// syncContentStatus applies the plan after other page writes. A fresh read
// catches status cleared by an update and avoids restoring over another status.
func (r *publishRun) syncContentStatus(ctx context.Context, target *confluence.PageInfo, change statusChange, settingsWritten bool, hash string) (bool, error) {
	if !change.declared && change.desired == nil {
		return false, nil
	}
	if settingsWritten {
		var err error
		change.current, err = r.api.ReadPageContentStatus(ctx, target.ID)
		if err != nil {
			return false, err
		}
	}
	if !change.differs() || (!change.declared && change.current != nil) {
		return false, nil
	}

	snapshot, err := r.api.GetPageByIDExpanded(ctx, target.ID, "version,body.storage")
	if err != nil {
		return false, err
	}
	if snapshot.Version.Number != target.Version.Number {
		return false, fmt.Errorf("page %s changed before writing content status: %w", target.ID, confluence.ErrConflict)
	}
	if hash != "" {
		if err := r.rememberStatusFingerprint(ctx, snapshot, hash); err != nil {
			if confluence.IsCancellation(err) {
				return false, err
			}
			logctx.From(ctx).Warn().Err(err).Msgf("unable to save content fingerprint for page %s", target.ID)
		}
	}
	if err := r.api.SetPageContentStatus(ctx, target, change.desired); err != nil {
		return false, err
	}
	return true, nil
}

// statusFingerprintProperty carries the body fingerprint across a status
// write, whose new page version has an empty version message.
const statusFingerprintProperty = "mark.content-status-fingerprint"

// statusFingerprint names the version and the exact stored body the hash is
// valid for, so a failed or concurrent write cannot hide a new page body.
type statusFingerprint struct {
	Version int64  `json:"version"`
	Storage string `json:"storage"`
	Hash    string `json:"hash"`
}

// rememberStatusFingerprint saves the hash before the status write so the
// status remains the final setting applied to the page.
func (r *publishRun) rememberStatusFingerprint(ctx context.Context, snapshot *confluence.PageInfo, hash string) error {
	property, err := r.api.GetPublishedPageProperty(ctx, snapshot.ID, statusFingerprintProperty)
	if err != nil {
		return err
	}
	value, err := json.Marshal(statusFingerprint{
		Version: snapshot.Version.Number + 1,
		Storage: fingerprint.SHA1(snapshot.Body.Storage.Value),
		Hash:    hash,
	})
	if err != nil {
		return err
	}
	return r.api.SetPublishedPageProperty(ctx, snapshot.ID, statusFingerprintProperty, value, property)
}

// previousContentHash reads the version message, falling back to a fingerprint
// left specifically for a status-generated version of the same stored body.
func (r *publishRun) previousContentHash(ctx context.Context, target *confluence.PageInfo) (string, error) {
	if target == nil {
		return "", nil
	}
	hash := fingerprint.FromVersionMessage(target.Version.Message)
	if target.Version.Message != "" || target.Version.Number < 2 || target.Type == confluence.ContentBlogPost {
		return hash, nil
	}
	cloud, err := r.api.Cloud(ctx)
	if !cloud {
		if confluence.IsCancellation(err) {
			return "", err
		}
		return "", nil
	}

	property, err := r.api.GetPublishedPageProperty(ctx, target.ID, statusFingerprintProperty)
	if confluence.IsCancellation(err) {
		return "", err
	}
	if statusReadUnavailable(err) {
		return "", nil
	}
	if err != nil {
		logctx.From(ctx).Warn().Err(err).Msgf("unable to read saved content fingerprint for page %s", target.ID)
		return "", nil
	}
	if property == nil {
		return "", nil
	}
	var recorded statusFingerprint
	matches := json.Unmarshal(property.Value, &recorded) == nil && recorded.Version == target.Version.Number
	if !matches {
		return "", nil
	}
	snapshot, err := r.api.GetPageByIDExpanded(ctx, target.ID, "version,body.storage")
	if err != nil {
		if confluence.IsCancellation(err) {
			return "", err
		}
		logctx.From(ctx).Warn().Err(err).Msgf("unable to verify saved content fingerprint for page %s", target.ID)
		return "", nil
	}
	if snapshot.Version.Number != recorded.Version || fingerprint.SHA1(snapshot.Body.Storage.Value) != recorded.Storage {
		return "", nil
	}
	return recorded.Hash, nil
}
