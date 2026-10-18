package config

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/BurntSushi/toml"
)

// ReadUserLayer returns only what the user file sets, without defaults.
func ReadUserLayer() (Config, error) {
	return readOptional(UserFile())
}

// UserSettings is the defaults with the user layer applied: the settings in
// effect outside any project.
func UserSettings(layer Config) Config {
	return merge(Defaults(), layer)
}

// LoadWithUserLayer is Load with layer in place of the user file, to try
// settings before saving them.
func LoadWithUserLayer(workDir string, layer Config) (Config, error) {
	project, err := ReadProject(workDir)
	if err != nil {
		return merge(Defaults(), layer), err
	}
	return combine(workDir, layer, project)
}

// LoadLayers is Load with both files given, to try them before saving. The
// project layer is the user's own, so it is trusted.
func LoadLayers(workDir string, user, project Config) (Config, error) {
	return combine(workDir, user, Project{Layer: project, Trusted: true})
}

func combine(workDir string, user Config, project Project) (Config, error) {
	cfg := merge(Defaults(), user)
	cfg.Permissions = cfg.Permissions.ForProject(workDir)
	if project.Trusted {
		cfg = WithProject(cfg, project.Layer)
	} else {
		cfg = mergeProject(cfg, project.Layer)
	}
	cfg = normalizeToolNames(cfg)
	return cfg, validate(cfg)
}

// SaveUserLayer writes layer as the user file, replacing it whole. Comments
// in the old file are lost.
func SaveUserLayer(layer Config) error {
	path := UserFile()
	var encoded bytes.Buffer
	if err := toml.NewEncoder(&encoded).Encode(setFields(reflect.ValueOf(layer))); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, encoded.Bytes(), 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

// setFields turns a config struct into a map of the fields it sets, keyed by
// their toml names. A nil list or pointer is unset; an empty list or a
// pointer to zero is set.
func setFields(value reflect.Value) map[string]any {
	fields := map[string]any{}
	for i := range value.NumField() {
		name, _, _ := strings.Cut(value.Type().Field(i).Tag.Get("toml"), ",")
		if field, isSet := setValue(value.Field(i)); isSet {
			fields[name] = field
		}
	}
	return fields
}

func setValue(value reflect.Value) (any, bool) {
	switch value.Kind() {
	case reflect.Pointer:
		if value.IsNil() {
			return nil, false
		}
		return value.Elem().Interface(), true
	case reflect.Struct:
		fields := setFields(value)
		return fields, len(fields) > 0
	case reflect.Map:
		if value.Len() == 0 {
			return nil, false
		}
		if value.Type().Elem().Kind() != reflect.Struct {
			return value.Interface(), true
		}
		entries := map[string]any{}
		for _, key := range value.MapKeys() {
			entry, _ := setValue(value.MapIndex(key))
			if entry == nil {
				entry = map[string]any{}
			}
			entries[key.String()] = entry
		}
		return entries, true
	case reflect.Slice:
		return value.Interface(), !value.IsNil()
	}
	return value.Interface(), !value.IsZero()
}
