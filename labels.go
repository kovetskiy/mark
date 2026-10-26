// Working out and applying label changes.

package mark

import (
	"fmt"
	"slices"
	"strings"

	"github.com/kovetskiy/mark/v16/confluence"
	"github.com/rs/zerolog/log"
)

// labelChanges reads a page's labels and says which updateLabels would add and
// which it would remove. A dry run asks it too, so the two agree.
func labelChanges(
	api *confluence.API,
	target *confluence.PageInfo,
	metaLabels []string,
	appendOnly bool,
) (add, del []string, err error) {
	// Nothing to add, and appending removes nothing: no need to read the labels.
	if appendOnly && len(metaLabels) == 0 {
		return nil, nil, nil
	}

	labelInfo, err := api.GetPageLabels(target, "global")
	if err != nil {
		return nil, nil, err
	}

	log.Debug().Msg("Page Labels:")
	log.Debug().Interface("labels", labelInfo.Labels).Send()
	log.Debug().Msg("Meta Labels:")
	log.Debug().Interface("labels", metaLabels).Send()

	if !appendOnly {
		del = determineLabelsToRemove(labelInfo, metaLabels)
	}
	log.Debug().Msg("Del Labels:")
	log.Debug().Interface("labels", del).Send()

	add = determineLabelsToAdd(metaLabels, labelInfo)
	log.Debug().Msg("Add Labels:")
	log.Debug().Interface("labels", add).Send()

	return add, del, nil
}

// updateLabels brings a page's labels in line with what its document asks for.
//
// Whether that means removing the others is the caller's choice. A page's
// labels are not only mark's to decide: teams label pages in Confluence to
// drive macros, searches and reports, and removing a label somebody added there
// destroys work no document ever mentioned. Appending leaves them, at the cost
// of a label outliving the Label header that introduced it -- which is visible
// and reversible, unlike the deletion.
func updateLabels(api *confluence.API, target *confluence.PageInfo, metaLabels []string, appendOnly bool) error {
	addLabels, delLabels, err := labelChanges(api, target, metaLabels, appendOnly)
	if err != nil {
		return err
	}

	if len(addLabels) > 0 {
		if _, err = api.AddPageLabels(target, addLabels); err != nil {
			return fmt.Errorf("error adding labels: %w", err)
		}
	}

	for _, label := range delLabels {
		if _, err = api.DeletePageLabel(target, label); err != nil {
			return fmt.Errorf("error deleting label %q: %w", label, err)
		}
	}

	return nil
}

func determineLabelsToRemove(labelInfo *confluence.LabelInfo, metaLabels []string) []string {
	var labels []string
	for _, label := range labelInfo.Labels {
		if !slices.ContainsFunc(metaLabels, func(metaLabel string) bool {
			return strings.EqualFold(metaLabel, label.Name)
		}) {
			labels = append(labels, label.Name)
		}
	}
	return labels
}

func determineLabelsToAdd(metaLabels []string, labelInfo *confluence.LabelInfo) []string {
	var labels []string
	for _, metaLabel := range metaLabels {
		if !slices.ContainsFunc(labelInfo.Labels, func(label confluence.Label) bool {
			return strings.EqualFold(label.Name, metaLabel)
		}) {
			labels = append(labels, metaLabel)
		}
	}
	return labels
}
