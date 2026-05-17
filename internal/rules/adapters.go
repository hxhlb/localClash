package rules

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"sort"
	"strings"
)

type githubEntry struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	DownloadURL string `json:"download_url"`
}

func adaptSource(ctx context.Context, source Source) (PackCache, error) {
	switch source.Adapter {
	case "sukkaw":
		return adaptSukkaW(ctx, source)
	case "blackmatrix7":
		return adaptBlackmatrix7(ctx, source)
	case "v2fly-dlc":
		return adaptV2FlyDLC(ctx, source)
	case "syncnext":
		return adaptSyncnext(source), nil
	default:
		return PackCache{}, fmt.Errorf("unknown adapter %q", source.Adapter)
	}
}

func adaptSukkaW(ctx context.Context, source Source) (PackCache, error) {
	groups := map[string]*Pack{}
	for _, kind := range []string{"domainset", "non_ip", "ip"} {
		files, err := listSukkaWSourceFiles(ctx, source.URL, kind)
		if err != nil {
			return PackCache{}, err
		}
		for _, file := range files {
			id, ok := trimConf(file.Name)
			if !ok {
				continue
			}
			pack := groups[id]
			if pack == nil {
				pack = &Pack{ID: id, Name: id, Renderable: true}
				groups[id] = pack
			}
			pack.Components = append(pack.Components, sukkawComponent(source, id, kind))
		}
	}

	packs := make([]Pack, 0, len(groups))
	for _, pack := range groups {
		sortComponents(pack.Components)
		packs = append(packs, *pack)
	}
	sort.Slice(packs, func(i, j int) bool { return packs[i].ID < packs[j].ID })
	return PackCache{Version: 1, Source: source.ID, Adapter: source.Adapter, Renderable: true, Packs: packs}, nil
}

func listSukkaWSourceFiles(ctx context.Context, sourceURL, kind string) ([]githubEntry, error) {
	apiURL, err := githubAPIContentsURL(strings.TrimRight(sourceURL, "/") + "/tree/master/Source/" + kind)
	if err != nil {
		return nil, err
	}
	return fetchGitHubEntries(ctx, apiURL)
}

func sukkawComponent(source Source, packID, kind string) Component {
	behavior := "classical"
	if kind == "domainset" {
		behavior = "domain"
	}
	fileName := packID + ".txt"
	return Component{
		ID:         kind,
		Behavior:   behavior,
		Format:     "text",
		OrderClass: kind,
		URL:        strings.TrimRight(source.BaseURL, "/") + "/Clash/" + kind + "/" + fileName,
		Path:       "./rule-packs/" + source.ID + "/" + packID + "_" + kind + ".txt",
	}
}

func adaptBlackmatrix7(ctx context.Context, source Source) (PackCache, error) {
	apiURL, err := githubAPIContentsURL(source.URL)
	if err != nil {
		return PackCache{}, err
	}
	entries, err := fetchGitHubEntries(ctx, apiURL)
	if err != nil {
		return PackCache{}, err
	}
	var packs []Pack
	for _, entry := range entries {
		if entry.Type != "dir" {
			continue
		}
		name := entry.Name
		component := Component{
			ID:         name,
			Behavior:   "classical",
			Format:     "yaml",
			OrderClass: "mixed",
			URL:        strings.TrimRight(source.RawBaseURL, "/") + "/" + pathEscape(name) + "/" + pathEscape(name) + ".yaml",
			Path:       "./rule-packs/" + source.ID + "/" + name + ".yaml",
		}
		packs = append(packs, Pack{ID: name, Name: name, Renderable: true, Components: []Component{component}})
	}
	sort.Slice(packs, func(i, j int) bool { return packs[i].ID < packs[j].ID })
	return PackCache{Version: 1, Source: source.ID, Adapter: source.Adapter, Renderable: true, Packs: packs}, nil
}

func adaptV2FlyDLC(ctx context.Context, source Source) (PackCache, error) {
	apiURL, err := githubAPIContentsURL(source.URL)
	if err != nil {
		return PackCache{}, err
	}
	entries, err := fetchGitHubEntries(ctx, apiURL)
	if err != nil {
		return PackCache{}, err
	}
	var packs []Pack
	for _, entry := range entries {
		if entry.Type != "file" {
			continue
		}
		packs = append(packs, Pack{
			ID:         entry.Name,
			Name:       entry.Name,
			Renderable: false,
			Reason:     "v2fly domain-list-community source data requires geosite conversion before Mihomo rule-provider rendering",
		})
	}
	sort.Slice(packs, func(i, j int) bool { return packs[i].ID < packs[j].ID })
	return PackCache{Version: 1, Source: source.ID, Adapter: source.Adapter, Renderable: false, Packs: packs}, nil
}

func adaptSyncnext(source Source) PackCache {
	rawBaseURL := strings.TrimRight(source.RawBaseURL, "/")
	packs := []Pack{
		syncnextPack(source.ID, "SyncnextProxy", "Proxy domains maintained for Ronnie's apps", "PROXY", rawBaseURL+"/proxy-classical.yaml"),
		syncnextPack(source.ID, "SyncnextUnbreak", "Direct/unbreak domains maintained for Ronnie's apps", "DIRECT", rawBaseURL+"/Unbreak-classical.yaml"),
	}
	return PackCache{Version: 1, Source: source.ID, Adapter: source.Adapter, Renderable: true, Packs: packs}
}

func syncnextPack(sourceID, id, name, target, url string) Pack {
	return Pack{
		ID:         id,
		Name:       name,
		Target:     target,
		Renderable: true,
		Components: []Component{
			{
				ID:         id,
				Behavior:   "classical",
				Format:     "yaml",
				OrderClass: "mixed",
				URL:        url,
				Path:       "./rule-packs/" + sourceID + "/" + id + ".yaml",
			},
		},
	}
}

func fetchGitHubEntries(ctx context.Context, apiURL string) ([]githubEntry, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "localclash-rules-adapter")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("github contents request failed: %s", resp.Status)
	}
	var entries []githubEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, err
	}
	return entries, nil
}

func sortComponents(components []Component) {
	order := map[string]int{"domainset": 0, "non_ip": 1, "ip": 2, "mixed": 3}
	sort.Slice(components, func(i, j int) bool {
		left := order[components[i].OrderClass]
		right := order[components[j].OrderClass]
		if left == right {
			return components[i].ID < components[j].ID
		}
		return left < right
	})
}

func pathEscape(value string) string {
	return strings.ReplaceAll(path.Clean(value), " ", "%20")
}
