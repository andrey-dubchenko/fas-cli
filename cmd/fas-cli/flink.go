package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// pageSize is the page size used when paging through Flink's schema list API.
const pageSize = 250

// API response types

type Metadata struct {
	PageNum   int `json:"pageNum"`
	PageSize  int `json:"pageSize"`
	PageCount int `json:"pageCount"`
	Count     int `json:"count"`
}

type SchemaShort struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Status  string `json:"status"`
	Module  string `json:"module"`
	JobID   string `json:"jobId"`
}

type SchemasResponse struct {
	Metadata Metadata      `json:"metadata"`
	Schemes  []SchemaShort `json:"schemes"`
}

type IndexAliasInfo struct {
	Aliases map[string]json.RawMessage `json:"aliases"`
}

type catIndexEntry struct {
	Index     string `json:"index"`
	DocsCount string `json:"docs.count"`
}

// Grouped model

type VersionItem struct {
	Version   string
	Status    string
	Selected  bool
	IndexName string
	Aliases   []string
	DocCount  string
}

type SchemaGroup struct {
	Name     string
	Versions []VersionItem
	Expanded bool
}

// Pending operation item

type opItem struct {
	name      string
	version   string
	indexName string
	subOp     string // for restart: "stop" or "start", empty for other ops
}

// Backend calls are synchronous. The App runs them on goroutines and marshals
// results back to the UI thread via Application.QueueUpdateDraw.

// schemaPath substitutes the "{name}"/"{version}" placeholders in a
// configured endpoint template with URL-escaped values.
func schemaPath(template, name, version string) string {
	p := strings.ReplaceAll(template, "{name}", url.PathEscape(name))
	p = strings.ReplaceAll(p, "{version}", url.PathEscape(version))
	return p
}

func fetchAliases(client *http.Client, opensearchURL string) (map[string][]string, error) {
	resp, err := client.Get(opensearchURL + "/_alias")
	if err != nil {
		return nil, fmt.Errorf("opensearch _alias request failed: %w", err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("opensearch _alias read body: %w", err)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("opensearch _alias HTTP %d: %s", resp.StatusCode, string(body))
	}

	var raw map[string]IndexAliasInfo
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("opensearch _alias decode: %w", err)
	}

	result := make(map[string][]string, len(raw))
	for indexName, info := range raw {
		var aliases []string
		for aliasName := range info.Aliases {
			aliases = append(aliases, aliasName)
		}
		sort.Strings(aliases)
		result[indexName] = aliases
	}
	return result, nil
}

// fetchDocCounts silently returns nil on any failure: document counts are a
// best-effort annotation, not worth blocking or erroring the whole UI over.
func fetchDocCounts(client *http.Client, opensearchURL string) map[string]string {
	resp, err := client.Get(opensearchURL + "/_cat/indices?format=json&h=index,docs.count")
	if err != nil {
		return nil
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil || resp.StatusCode != 200 {
		return nil
	}

	var entries []catIndexEntry
	if err := json.Unmarshal(body, &entries); err != nil {
		return nil
	}

	counts := make(map[string]string, len(entries))
	for _, e := range entries {
		counts[e.Index] = e.DocsCount
	}
	return counts
}

func fetchSchemas(client *http.Client, flinkURL, opensearchURL, module string, endpoints Endpoints) ([]SchemaGroup, error) {
	var allSchemas []SchemaShort
	page := 0
	for {
		u, _ := url.Parse(flinkURL + endpoints.SchemaList)
		q := u.Query()
		if module != "" {
			// No module means no filter: show jobs from every module.
			q.Set("module", module)
		}
		q.Set("page_num", strconv.Itoa(page))
		q.Set("page_size", strconv.Itoa(pageSize))
		u.RawQuery = q.Encode()

		resp, err := client.Get(u.String())
		if err != nil {
			return nil, fmt.Errorf("flink request failed: %w", err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("flink read body: %w", err)
		}
		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("flink HTTP %d: %s", resp.StatusCode, string(body))
		}

		var result SchemasResponse
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("flink decode: %w", err)
		}
		allSchemas = append(allSchemas, result.Schemes...)

		if page+1 >= result.Metadata.PageCount {
			break
		}
		page++
	}

	if opensearchURL == "" {
		// No OpenSearch configured for this environment: schemas just won't
		// show an index/alias, rather than failing the whole load.
		return groupSchemas(allSchemas, nil), nil
	}

	aliases, err := fetchAliases(client, opensearchURL)
	if err != nil {
		return nil, err
	}

	return groupSchemas(allSchemas, aliases), nil
}

func findIndex(name, version string, aliases map[string][]string) (string, []string) {
	candidates := []string{
		strings.ToLower(name) + "-" + strings.ToLower(version),
		strings.ToLower(name) + "_" + strings.ToLower(version),
		name + "-" + version,
		name + "_" + version,
	}
	seen := make(map[string]bool)
	for _, c := range candidates {
		if seen[c] {
			continue
		}
		seen[c] = true
		if a, ok := aliases[c]; ok {
			return c, a
		}
	}
	return "", nil
}

func groupSchemas(schemas []SchemaShort, aliases map[string][]string) []SchemaGroup {
	grouped := make(map[string][]VersionItem)
	for _, s := range schemas {
		idxName, idxAliases := findIndex(s.Name, s.Version, aliases)
		grouped[s.Name] = append(grouped[s.Name], VersionItem{
			Version:   s.Version,
			Status:    s.Status,
			IndexName: idxName,
			Aliases:   idxAliases,
		})
	}

	groups := make([]SchemaGroup, 0, len(grouped))
	for name, versions := range grouped {
		sort.Slice(versions, func(i, j int) bool {
			return versions[i].Version < versions[j].Version
		})
		groups = append(groups, SchemaGroup{
			Name:     name,
			Versions: versions,
		})
	}
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Name < groups[j].Name
	})
	return groups
}

// processNextOp performs one HTTP step for opType against item and returns
// the log lines produced. For "restart" it returns nextOp non-nil after the
// stop half completes, so the caller can chain the start half.
func processNextOp(client *http.Client, flinkURL, opensearchURL, opType string, item opItem, endpoints Endpoints) (lines []string, nextOp *opItem) {
	switch opType {
	case "restart":
		// Handle restart as a composite operation: stop then start
		if item.subOp == "" {
			item.subOp = "stop"
		}
		if item.subOp == "stop" {
			u := flinkURL + schemaPath(endpoints.SchemaStop, item.name, item.version)
			req, _ := http.NewRequest("POST", u, nil)
			resp, err := client.Do(req)
			if err != nil {
				lines = append(lines, fmt.Sprintf("  FAIL %s/%s: %v", item.name, item.version, err))
			} else {
				body, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				if resp.StatusCode == 204 {
					lines = append(lines, fmt.Sprintf("  OK   %s/%s stopped", item.name, item.version))
					// Queue the start operation
					item.subOp = "start"
					return lines, &item
				}
				lines = append(lines, fmt.Sprintf("  FAIL %s/%s: HTTP %d %s", item.name, item.version, resp.StatusCode, string(body)))
			}
		} else if item.subOp == "start" {
			u := flinkURL + schemaPath(endpoints.SchemaStart, item.name, item.version)
			reqBody := []byte(`{"startArg":[]}`)
			req, _ := http.NewRequest("POST", u, bytes.NewReader(reqBody))
			req.Header.Set("Content-Type", "application/json")
			resp, err := client.Do(req)
			if err != nil {
				lines = append(lines, fmt.Sprintf("  FAIL %s/%s start: %v", item.name, item.version, err))
			} else {
				body, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				if resp.StatusCode == 204 {
					lines = append(lines, fmt.Sprintf("  OK   %s/%s started", item.name, item.version))
				} else {
					lines = append(lines, fmt.Sprintf("  FAIL %s/%s start: HTTP %d %s", item.name, item.version, resp.StatusCode, string(body)))
				}
			}
		}

	case "stop":
		u := flinkURL + schemaPath(endpoints.SchemaStop, item.name, item.version)
		req, _ := http.NewRequest("POST", u, nil)
		resp, err := client.Do(req)
		if err != nil {
			lines = append(lines, fmt.Sprintf("  FAIL %s/%s: %v", item.name, item.version, err))
		} else {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode == 204 {
				lines = append(lines, fmt.Sprintf("  OK   %s/%s stopped", item.name, item.version))
			} else {
				lines = append(lines, fmt.Sprintf("  FAIL %s/%s: HTTP %d %s", item.name, item.version, resp.StatusCode, string(body)))
			}
		}

	case "delete":
		u := flinkURL + schemaPath(endpoints.SchemaDelete, item.name, item.version) + "?deleteIndex=false"
		req, _ := http.NewRequest("DELETE", u, nil)
		resp, err := client.Do(req)
		if err != nil {
			lines = append(lines, fmt.Sprintf("  FAIL %s/%s schema delete: %v", item.name, item.version, err))
			return lines, nil
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			lines = append(lines, fmt.Sprintf("  OK   %s/%s schema deleted", item.name, item.version))
		} else {
			lines = append(lines, fmt.Sprintf("  FAIL %s/%s schema delete: HTTP %d %s", item.name, item.version, resp.StatusCode, string(body)))
			return lines, nil
		}

		if item.indexName != "" {
			idxURL := fmt.Sprintf("%s/%s", opensearchURL, url.PathEscape(item.indexName))
			req, _ := http.NewRequest("DELETE", idxURL, nil)
			resp, err := client.Do(req)
			if err != nil {
				lines = append(lines, fmt.Sprintf("  FAIL %s/%s index delete (%s): %v", item.name, item.version, item.indexName, err))
			} else {
				body, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				if resp.StatusCode >= 200 && resp.StatusCode < 300 {
					lines = append(lines, fmt.Sprintf("  OK   %s/%s index deleted (%s)", item.name, item.version, item.indexName))
				} else {
					lines = append(lines, fmt.Sprintf("  FAIL %s/%s index delete (%s): HTTP %d %s", item.name, item.version, item.indexName, resp.StatusCode, string(body)))
				}
			}
		}
	}

	return lines, nil
}

func collectSelected(groups []SchemaGroup) []opItem {
	var items []opItem
	for _, g := range groups {
		for _, v := range g.Versions {
			if v.Selected {
				items = append(items, opItem{
					name:      g.Name,
					version:   v.Version,
					indexName: v.IndexName,
				})
			}
		}
	}
	return items
}

func moveAlias(client *http.Client, opensearchURL, srcIndex, dstIndex string, aliases []string) []string {
	type aliasAction struct {
		Index string `json:"index"`
		Alias string `json:"alias"`
	}
	type action struct {
		Remove *aliasAction `json:"remove,omitempty"`
		Add    *aliasAction `json:"add,omitempty"`
	}
	type aliasRequest struct {
		Actions []action `json:"actions"`
	}

	var actions []action
	for _, alias := range aliases {
		actions = append(actions,
			action{Remove: &aliasAction{Index: srcIndex, Alias: alias}},
			action{Add: &aliasAction{Index: dstIndex, Alias: alias}},
		)
	}

	reqBody, _ := json.Marshal(aliasRequest{Actions: actions})
	req, _ := http.NewRequest("POST", opensearchURL+"/_aliases", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return []string{fmt.Sprintf("  FAIL alias move: %v", err)}
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	var lines []string
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		for _, alias := range aliases {
			lines = append(lines, fmt.Sprintf("  OK   alias %s: %s -> %s", alias, srcIndex, dstIndex))
		}
	} else {
		lines = append(lines, fmt.Sprintf("  FAIL alias move: HTTP %d %s", resp.StatusCode, string(body)))
	}
	return lines
}
