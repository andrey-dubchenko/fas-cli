package flink

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
	"time"

	"fas-cli/internal/config"
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

type OpItem struct {
	Name      string
	Version   string
	IndexName string
	SubOp     string // for restart: "stop" or "start", empty for other ops
}

// Backend calls are synchronous. The App runs them on goroutines and marshals
// results back to the UI thread via Application.QueueUpdateDraw.

// SchemaPath substitutes the "{name}"/"{version}" placeholders in a
// configured endpoint template with URL-escaped values.
func SchemaPath(template, name, version string) string {
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

// FetchDocCounts silently returns nil on any failure: document counts are a
// best-effort annotation, not worth blocking or erroring the whole UI over.
func FetchDocCounts(client *http.Client, opensearchURL string) map[string]string {
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

func FetchSchemas(client *http.Client, flinkURL, opensearchURL, module string, endpoints config.Endpoints) ([]SchemaGroup, error) {
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
		return GroupSchemas(allSchemas, nil), nil
	}

	aliases, err := fetchAliases(client, opensearchURL)
	if err != nil {
		return nil, err
	}

	return GroupSchemas(allSchemas, aliases), nil
}

func FindIndex(name, version string, aliases map[string][]string) (string, []string) {
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

func GroupSchemas(schemas []SchemaShort, aliases map[string][]string) []SchemaGroup {
	grouped := make(map[string][]VersionItem)
	for _, s := range schemas {
		idxName, idxAliases := FindIndex(s.Name, s.Version, aliases)
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
			return CompareVersions(versions[i].Version, versions[j].Version) < 0
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

// ProcessNextOp performs one HTTP step for opType against item and returns
// the log lines produced. For "restart" it returns nextOp non-nil after the
// stop half completes, so the caller can chain the start half.
func ProcessNextOp(client *http.Client, flinkURL, opensearchURL, opType string, item OpItem, endpoints config.Endpoints) (lines []string, nextOp *OpItem) {
	switch opType {
	case "restart":
		// Handle restart as a composite operation: stop then start
		if item.SubOp == "" {
			item.SubOp = "stop"
		}
		if item.SubOp == "stop" {
			l, ok := stopSchema(client, flinkURL, item, endpoints)
			lines = append(lines, l)
			if ok {
				// Queue the start operation
				item.SubOp = "start"
				return lines, &item
			}
		} else if item.SubOp == "start" {
			// The stop endpoint can return before the job has actually
			// stopped, in which case an immediate start is rejected. Retry a
			// few times with a short pause before reporting failure.
			const attempts = 3
			for i := 1; i <= attempts; i++ {
				status, body, err := callAPI(client, "POST", flinkURL+SchemaPath(endpoints.SchemaStart, item.Name, item.Version), []byte(`{"startArg":[]}`))
				if err == nil && status >= 200 && status < 300 {
					lines = append(lines, fmt.Sprintf("  OK   %s/%s started", item.Name, item.Version))
					break
				}
				if i == attempts {
					if err != nil {
						lines = append(lines, fmt.Sprintf("  FAIL %s/%s start: %v", item.Name, item.Version, err))
					} else {
						lines = append(lines, fmt.Sprintf("  FAIL %s/%s start: HTTP %d %s", item.Name, item.Version, status, body))
					}
					break
				}
				time.Sleep(2 * time.Second)
			}
		}

	case "stop":
		l, _ := stopSchema(client, flinkURL, item, endpoints)
		lines = append(lines, l)

	case "delete":
		u := flinkURL + SchemaPath(endpoints.SchemaDelete, item.Name, item.Version) + "?deleteIndex=false"
		req, _ := http.NewRequest("DELETE", u, nil)
		resp, err := client.Do(req)
		if err != nil {
			lines = append(lines, fmt.Sprintf("  FAIL %s/%s schema delete: %v", item.Name, item.Version, err))
			return lines, nil
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			lines = append(lines, fmt.Sprintf("  OK   %s/%s schema deleted", item.Name, item.Version))
		} else {
			lines = append(lines, fmt.Sprintf("  FAIL %s/%s schema delete: HTTP %d %s", item.Name, item.Version, resp.StatusCode, string(body)))
			return lines, nil
		}

		if item.IndexName != "" {
			idxURL := fmt.Sprintf("%s/%s", opensearchURL, url.PathEscape(item.IndexName))
			req, _ := http.NewRequest("DELETE", idxURL, nil)
			resp, err := client.Do(req)
			if err != nil {
				lines = append(lines, fmt.Sprintf("  FAIL %s/%s index delete (%s): %v", item.Name, item.Version, item.IndexName, err))
			} else {
				body, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				if resp.StatusCode >= 200 && resp.StatusCode < 300 {
					lines = append(lines, fmt.Sprintf("  OK   %s/%s index deleted (%s)", item.Name, item.Version, item.IndexName))
				} else {
					lines = append(lines, fmt.Sprintf("  FAIL %s/%s index delete (%s): HTTP %d %s", item.Name, item.Version, item.IndexName, resp.StatusCode, string(body)))
				}
			}
		}
	}

	return lines, nil
}

// callAPI performs one request and returns the status code and body text.
func callAPI(client *http.Client, method, u string, body []byte) (int, string, error) {
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, u, rdr)
	if err != nil {
		return 0, "", err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b), nil
}

// stopSchema stops one schema and returns the log line plus whether it
// succeeded. Any 2xx counts as success.
func stopSchema(client *http.Client, flinkURL string, item OpItem, endpoints config.Endpoints) (string, bool) {
	status, body, err := callAPI(client, "POST", flinkURL+SchemaPath(endpoints.SchemaStop, item.Name, item.Version), nil)
	switch {
	case err != nil:
		return fmt.Sprintf("  FAIL %s/%s: %v", item.Name, item.Version, err), false
	case status >= 200 && status < 300:
		return fmt.Sprintf("  OK   %s/%s stopped", item.Name, item.Version), true
	default:
		return fmt.Sprintf("  FAIL %s/%s: HTTP %d %s", item.Name, item.Version, status, body), false
	}
}

// CompareVersions orders version strings naturally: digit runs compare as
// numbers, so "1.9" < "1.10" and "2" < "10".
func CompareVersions(a, b string) int {
	for a != "" && b != "" {
		da, db := a[0] >= '0' && a[0] <= '9', b[0] >= '0' && b[0] <= '9'
		if da && db {
			na, ra := splitDigits(a)
			nb, rb := splitDigits(b)
			na, nb = strings.TrimLeft(na, "0"), strings.TrimLeft(nb, "0")
			if len(na) != len(nb) {
				if len(na) < len(nb) {
					return -1
				}
				return 1
			}
			if c := strings.Compare(na, nb); c != 0 {
				return c
			}
			a, b = ra, rb
			continue
		}
		if a[0] != b[0] {
			if a[0] < b[0] {
				return -1
			}
			return 1
		}
		a, b = a[1:], b[1:]
	}
	return strings.Compare(a, b)
}

// splitDigits splits s into its leading digit run and the remainder.
func splitDigits(s string) (string, string) {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	return s[:i], s[i:]
}

func CollectSelected(groups []SchemaGroup) []OpItem {
	var items []OpItem
	for _, g := range groups {
		for _, v := range g.Versions {
			if v.Selected {
				items = append(items, OpItem{
					Name:      g.Name,
					Version:   v.Version,
					IndexName: v.IndexName,
				})
			}
		}
	}
	return items
}

func MoveAlias(client *http.Client, opensearchURL, srcIndex, dstIndex string, aliases []string) []string {
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
