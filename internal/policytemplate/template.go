package policytemplate

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"localclash/internal/localconfig"

	"gopkg.in/yaml.v3"
)

const (
	DefaultDir                = "policy-templates"
	TemplateMinimal           = "minimal"
	TemplateLocalClashDefault = "localclash-default"
)

type File struct {
	ID          string             `yaml:"id" json:"id"`
	Name        string             `yaml:"name" json:"name"`
	Description string             `yaml:"description" json:"description"`
	Default     bool               `yaml:"default,omitempty" json:"default,omitempty"`
	Config      localconfig.Config `yaml:"config" json:"config"`
	Path        string             `yaml:"-" json:"path,omitempty"`
}

type Summary struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Default     bool   `json:"default,omitempty"`
	Path        string `json:"path,omitempty"`
}

func List(dir string) ([]Summary, error) {
	templates, err := loadAll(dir)
	if err != nil {
		return nil, err
	}
	out := make([]Summary, 0, len(templates))
	for _, template := range templates {
		out = append(out, summaryFor(template))
	}
	return out, nil
}

func Build(dir, id string) (localconfig.Config, Summary, error) {
	id = normalizeID(id)
	templates, err := loadAll(dir)
	if err != nil {
		return localconfig.Config{}, Summary{}, err
	}
	for _, template := range templates {
		if template.ID != id {
			continue
		}
		config := template.Config
		if config.Version == 0 {
			config.Version = 1
		}
		if strings.TrimSpace(config.PolicyTemplate) == "" {
			config.PolicyTemplate = template.ID
		}
		return config, summaryFor(template), nil
	}
	return localconfig.Config{}, Summary{}, fmt.Errorf("unknown policy_template %q; supported templates: %s", id, strings.Join(idsFromTemplates(templates), ", "))
}

func IDs(dir string) ([]string, error) {
	templates, err := loadAll(dir)
	if err != nil {
		return nil, err
	}
	return idsFromTemplates(templates), nil
}

func loadAll(dir string) ([]File, error) {
	dir = normalizeDir(dir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var templates []File
	for _, entry := range entries {
		if entry.IsDir() || shouldSkip(entry.Name()) {
			continue
		}
		template, err := loadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		templates = append(templates, template)
	}
	sort.Slice(templates, func(i, j int) bool { return templates[i].ID < templates[j].ID })
	if len(templates) == 0 {
		return nil, fmt.Errorf("no policy templates found in %q", dir)
	}
	return templates, nil
}

func loadFile(path string) (File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return File{}, err
	}
	var template File
	if err := yaml.Unmarshal(data, &template); err != nil {
		return File{}, fmt.Errorf("%s: %w", path, err)
	}
	template.Path = path
	if err := validate(template, path); err != nil {
		return File{}, err
	}
	return template, nil
}

func validate(template File, path string) error {
	if strings.TrimSpace(template.ID) == "" {
		return fmt.Errorf("%s: id is required", path)
	}
	if strings.TrimSpace(template.Name) == "" {
		return fmt.Errorf("%s: name is required", path)
	}
	if template.Config.Version == 0 {
		template.Config.Version = 1
	}
	if strings.TrimSpace(template.Config.PolicyTemplate) != "" && template.Config.PolicyTemplate != template.ID {
		return fmt.Errorf("%s: config.policy_template must be empty or match id %q", path, template.ID)
	}
	return nil
}

func summaryFor(template File) Summary {
	return Summary{
		ID:          template.ID,
		Name:        template.Name,
		Description: template.Description,
		Default:     template.Default,
		Path:        filepath.ToSlash(template.Path),
	}
}

func idsFromTemplates(templates []File) []string {
	ids := make([]string, 0, len(templates))
	for _, template := range templates {
		ids = append(ids, template.ID)
	}
	sort.Strings(ids)
	return ids
}

func normalizeID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return TemplateMinimal
	}
	return id
}

func normalizeDir(dir string) string {
	if strings.TrimSpace(dir) == "" {
		return DefaultDir
	}
	return dir
}

func shouldSkip(name string) bool {
	if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "._") {
		return true
	}
	ext := strings.ToLower(filepath.Ext(name))
	return ext != ".yaml" && ext != ".yml"
}
