package l10n

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

//go:embed locales/*.json
var localesFS embed.FS

var bundle *i18n.Bundle

// Init initializes the i18n bundle by loading JSON files from the embedded filesystem.
func Init() error {
	bundle = i18n.NewBundle(language.English)
	bundle.RegisterUnmarshalFunc("json", json.Unmarshal)

	err := fs.WalkDir(localesFS, "locales", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d != nil && !d.IsDir() && filepath.Ext(path) == ".json" {
			data, err := localesFS.ReadFile(path)
			if err != nil {
				return fmt.Errorf("failed to read embedded file %s: %w", path, err)
			}
			_, err = bundle.ParseMessageFileBytes(data, path)
			if err != nil {
				return fmt.Errorf("failed to parse message file %s: %w", path, err)
			}
		}
		return nil
	})

	return err
}

// T translates a message with the given ID and optional template data.
func T(lang, id string, data ...any) string {
	if bundle == nil {
		return id
	}

	localizer := i18n.NewLocalizer(bundle, lang, "en")
	cfg := &i18n.LocalizeConfig{
		MessageID: id,
	}

	if len(data) > 0 {
		cfg.TemplateData = data[0]
	}

	translated, err := localizer.Localize(cfg)
	if err != nil {
		return id
	}

	return translated
}
