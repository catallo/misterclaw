package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/catallo/misterclaw/pkg/mister"
)

const Version = "0.2.0"

// GitHub API types

type ghRepo struct {
	Name     string `json:"name"`
	FullName string `json:"full_name"`
	Archived bool   `json:"archived"`
	Fork     bool   `json:"fork"`
}

type ghContent struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	DownloadURL string `json:"download_url"`
	Type        string `json:"type"`
	SHA         string `json:"sha"`
}

// Repos to skip — these are infrastructure, not FPGA cores.
var skipRepos = map[string]bool{
	"Main_MiSTer":         true,
	"Distribution_MiSTer": true,
	"Downloader_MiSTer":   true,
	"Menu_MiSTer":         true,
	"Hardware_MiSTer":     true,
	"Wiki_MiSTer":         true,
	"Setup_MiSTer":        true,
	"Updater_MiSTer":      true,
	"Filters_MiSTer":      true,
	"Presets_MiSTer":      true,
	"Scripts_MiSTer":      true,
	"Template_MiSTer":     true,
	"MkDocs_MiSTer":       true,
	"mr-fusion":           true,
	"Linux-Kernel_MiSTer": true,
	"Quartus_Compile":     true,
	"SD-Installer-Win64_MiSTer": true,
	"Fonts_MiSTer":              true,
	"Gamecontrollerdb_MiSTer":   true,
	"WebMenu_MiSTer":            true,
}

// theypsilon repos that are actual FPGA cores (not infrastructure/scripts).
var theypsilonCoreRepos = []string{
	"Arcade-Freeze_MiSTer",
	"Arcade-TMNT_MiSTer",
	"CoCo3_MiSTer",
}
// va7deo repos — Toaplan, SNK68, Alpha68k, etc. (Coin-Op Collection source)
var va7deoCoreRepos = []string{
	"zerowing",
	"vimana",
	"rallybike",
	"demonswld",
	"SNK68",
	"alpha68k",
	"MegaSys1_A",
	"NextSpace",
	"TerraCresta",
	"PrehistoricIsle",
	"ArmedF",
}

// zakk4223 repos — Raizing/8ing arcade cores (Coin-Op Collection source)
var zakk4223CoreRepos = []string{
	"Arcade-Raizing_MiSTer",
}


func main() {
	output := flag.String("output", "confstr_db.json", "Output JSON file path")
	tokenFile := flag.String("token", "", "Path to file containing GitHub token")
	cacheDir := flag.String("cache-dir", "", "Directory to cache downloaded .sv files")
	versionFlag := flag.Bool("version", false, "Print version and exit")
	flag.Parse()

	if *versionFlag {
		fmt.Printf("confstr-update v%s\n", Version)
		os.Exit(0)
	}

	log.SetOutput(os.Stderr)
	log.SetFlags(log.Ltime | log.Lmsgprefix)
	log.SetPrefix("[confstr-update] ")

	token := ""
	if *tokenFile != "" {
		data, err := os.ReadFile(*tokenFile)
		if err != nil {
			log.Fatalf("reading token file: %v", err)
		}
		token = strings.TrimSpace(string(data))
	}

	if *cacheDir != "" {
		if err := os.MkdirAll(*cacheDir, 0755); err != nil {
			log.Fatalf("creating cache dir: %v", err)
		}
	}

	client := &http.Client{Timeout: 30 * time.Second}

	var allCores []mister.CoreOSD

	// --- Source 1: MiSTer-devel org (Verilog scraping) ---
	log.Println("=== Source 1: MiSTer-devel ===")
	repos := listOrgRepos(client, token, "MiSTer-devel")
	log.Printf("found %d repos in MiSTer-devel", len(repos))

	misterDevelCount := 0
	for _, repo := range repos {
		if skipRepos[repo.Name] || repo.Archived {
			continue
		}
		if !strings.HasSuffix(repo.Name, "_MiSTer") {
			continue
		}
		core := processRepo(client, token, *cacheDir, repo)
		if core != nil {
			allCores = append(allCores, *core)
			misterDevelCount++
			log.Printf("  [OK] %s -> %s (%d menu items)", repo.FullName, core.CoreName, len(core.Menu))
		}
	}
	log.Printf("MiSTer-devel: %d cores", misterDevelCount)

	// --- Source 2: Jotego jtcores monorepo (template-based) ---
	log.Println("=== Source 2: Jotego jtcores ===")
	jtCores := processJotegoCores(client, token, *cacheDir)
	allCores = append(allCores, jtCores...)
	log.Printf("Jotego: %d cores", len(jtCores))

	// --- Source 3: theypsilon individual repos ---
	log.Println("=== Source 3: theypsilon ===")
	theypsilonCount := 0
	for _, repoName := range theypsilonCoreRepos {
		repo := ghRepo{
			Name:     repoName,
			FullName: "theypsilon/" + repoName,
		}
		core := processRepo(client, token, *cacheDir, repo)
		if core != nil {
			allCores = append(allCores, *core)
			theypsilonCount++
			log.Printf("  [OK] %s -> %s (%d menu items)", repo.FullName, core.CoreName, len(core.Menu))
		}
	}
	log.Printf("theypsilon: %d cores", theypsilonCount)

	// --- Source 4: va7deo individual repos (Coin-Op Collection source) ---
	log.Println("=== Source 4: va7deo ===")
	va7deoCount := 0
	for _, repoName := range va7deoCoreRepos {
		repo := ghRepo{
			Name:     repoName,
			FullName: "va7deo/" + repoName,
		}
		core := processRepo(client, token, *cacheDir, repo)
		if core != nil {
			allCores = append(allCores, *core)
			va7deoCount++
			log.Printf("  [OK] %s -> %s (%d menu items)", repo.FullName, core.CoreName, len(core.Menu))
		}
	}
	log.Printf("va7deo: %d cores", va7deoCount)

	// --- Source 5: zakk4223 individual repos (Raizing/8ing, Coin-Op Collection source) ---
	log.Println("=== Source 5: zakk4223 ===")
	zakk4223Count := 0
	for _, repoName := range zakk4223CoreRepos {
		repo := ghRepo{
			Name:     repoName,
			FullName: "zakk4223/" + repoName,
		}
		core := processRepo(client, token, *cacheDir, repo)
		if core != nil {
			allCores = append(allCores, *core)
			zakk4223Count++
			log.Printf("  [OK] %s -> %s (%d menu items)", repo.FullName, core.CoreName, len(core.Menu))
		}
	}
	log.Printf("zakk4223: %d cores", zakk4223Count)

	db := mister.ConfStrDB{
		Version: time.Now().UTC().Format("2006-01-02"),
		Cores:   allCores,
	}

	data, err := json.MarshalIndent(db, "", "  ")
	if err != nil {
		log.Fatalf("marshaling JSON: %v", err)
	}

	if err := os.WriteFile(*output, data, 0644); err != nil {
		log.Fatalf("writing output: %v", err)
	}

	log.Printf("wrote %d cores to %s", len(allCores), *output)
}

// --- Jotego jtcores monorepo processing ---

// processJotegoCores downloads the cfgstr template from jtcores,
// then for each core under cores/*/cfg/macros.def, parses macros
// and generates CONF_STR from the template.
func processJotegoCores(client *http.Client, token, cacheDir string) []mister.CoreOSD {
	// Download the cfgstr Go template
	tmplURL := "https://raw.githubusercontent.com/jotego/jtcores/master/modules/jtframe/target/mister/cfgstr"
	tmplContent := downloadRaw(client, token, tmplURL)
	if tmplContent == "" {
		log.Println("  [WARN] could not download jtcores cfgstr template")
		return nil
	}

	tmpl, err := template.New("cfgstr").Option("missingkey=zero").Parse(tmplContent)
	if err != nil {
		log.Printf("  [WARN] parsing cfgstr template: %v", err)
		return nil
	}

	// List all core directories under cores/
	var coreDirs []ghContent
	apiURL := "https://api.github.com/repos/jotego/jtcores/contents/cores"
	if err := ghGet(client, token, apiURL, &coreDirs); err != nil {
		log.Printf("  [WARN] listing jtcores/cores: %v", err)
		return nil
	}

	var cores []mister.CoreOSD
	for _, dir := range coreDirs {
		if dir.Type != "dir" || dir.Name == ".gitignore" {
			continue
		}
		core := processJotegoCore(client, token, cacheDir, tmpl, dir.Name)
		if core != nil {
			cores = append(cores, *core)
			log.Printf("  [OK] jtcores/%s -> %s (%d menu items)", dir.Name, core.CoreName, len(core.Menu))
		}
	}
	return cores
}

// processJotegoCore processes a single Jotego core from the jtcores monorepo.
func processJotegoCore(client *http.Client, token, cacheDir string, tmpl *template.Template, coreName string) *mister.CoreOSD {
	// Download macros.def
	macrosURL := fmt.Sprintf("https://raw.githubusercontent.com/jotego/jtcores/master/cores/%s/cfg/macros.def", coreName)
	macrosContent := downloadRaw(client, token, macrosURL)
	if macrosContent == "" {
		return nil
	}

	// Parse macros, resolving includes
	macros := parseJotegoMacros(client, token, coreName, macrosContent)
	if macros["CORENAME"] == nil {
		// No CORENAME defined — skip
		return nil
	}

	// Add SEPARATOR (always ";")
	macros["SEPARATOR"] = ";"
	// Add a default JTFRAME_COMMIT if missing
	if macros["JTFRAME_COMMIT"] == nil {
		macros["JTFRAME_COMMIT"] = ""
	}

	// Execute the template
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, macros); err != nil {
		log.Printf("  [WARN] jtcores/%s template exec: %v", coreName, err)
		return nil
	}

	// Clean up the generated CONF_STR: collapse whitespace, normalize separators
	raw := cleanGeneratedConfStr(buf.String())
	if raw == "" {
		return nil
	}

	coreDisplayName := ""
	if v, ok := macros["CORENAME"].(string); ok {
		coreDisplayName = v
	}
	if coreDisplayName == "" {
		coreDisplayName = mister.ExtractCoreName(raw)
	}

	menu := mister.ParseConfStr(raw)
	return &mister.CoreOSD{
		CoreName:   coreDisplayName,
		Repo:       "jotego/jtcores",
		ConfStrRaw: raw,
		Menu:       menu,
	}
}

// parseJotegoMacros parses a macros.def file into a map for template execution.
// Handles include directives, section blocks ([mister], [*], etc.), and key=value pairs.
// Boolean macros (no value) become true. String macros become their value.
func parseJotegoMacros(client *http.Client, token, coreName, content string) map[string]interface{} {
	macros := make(map[string]interface{})
	parseJotegoMacrosInto(client, token, coreName, content, macros, 0)
	return macros
}

func parseJotegoMacrosInto(client *http.Client, token, coreName, content string, macros map[string]interface{}, depth int) {
	if depth > 5 {
		return // prevent infinite include loops
	}

	// Track which section we're in.
	// [*] = all platforms (default), [mister] = MiSTer-specific, others = skip.
	activeSection := true // start in global section (or [*])

	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Section header: [mister], [*], [mist|sidi], etc.
		if strings.HasPrefix(line, "[") {
			idx := strings.Index(line, "]")
			if idx > 0 {
				section := line[1:idx]
				activeSection = isMisterSection(section)
			}
			continue
		}

		// Only process lines in [*] or [mister] sections
		if !activeSection {
			continue
		}

		// Include directive
		if strings.HasPrefix(line, "include ") {
			includeFile := strings.TrimSpace(line[8:])
			includeURL := fmt.Sprintf("https://raw.githubusercontent.com/jotego/jtcores/master/cores/%s/cfg/%s", coreName, includeFile)
			includeContent := downloadRaw(client, token, includeURL)
			if includeContent != "" {
				parseJotegoMacrosInto(client, token, coreName, includeContent, macros, depth+1)
			}
			continue
		}

		// Key=value, key+=value (append), or bare key (boolean)
		if idx := strings.Index(line, "="); idx > 0 {
			key := strings.TrimSpace(line[:idx])
			value := strings.TrimSpace(line[idx+1:])
			// Handle += append syntax (e.g. CORE_OSD+=O7,Option,A,B)
			if strings.HasSuffix(key, "+") {
				key = strings.TrimSuffix(key, "+")
				if prev, ok := macros[key].(string); ok {
					value = prev + value
				}
			}
			macros[key] = value
		} else {
			// Bare macro name = boolean true
			// Skip non-macro lines (e.g. bare words that are section-like but not valid)
			if isValidMacroName(line) {
				macros[line] = true
			}
		}
	}
}

// isMisterSection returns true if the section name includes mister or is the global [*] section.
func isMisterSection(section string) bool {
	section = strings.TrimSpace(section)
	if section == "*" {
		return true
	}
	// Section can be like "mister" or "mist|sidi|mister"
	for _, part := range strings.Split(section, "|") {
		if strings.TrimSpace(part) == "mister" {
			return true
		}
	}
	return false
}

// isValidMacroName checks if a string looks like a valid macro name (uppercase, digits, underscores).
func isValidMacroName(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if !((c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_') {
			return false
		}
	}
	return true
}

// cleanGeneratedConfStr normalizes the raw output from template execution:
// collapses whitespace, removes empty lines, joins into semicolon-delimited string.
func cleanGeneratedConfStr(s string) string {
	var parts []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts = append(parts, line)
	}
	// Join all parts with spaces, then normalize semicolons
	joined := strings.Join(parts, " ")

	// Collapse multiple spaces
	for strings.Contains(joined, "  ") {
		joined = strings.ReplaceAll(joined, "  ", " ")
	}

	// Collapse multiple semicolons (e.g. ";;;" -> ";")
	for strings.Contains(joined, ";;") {
		joined = strings.ReplaceAll(joined, ";;", ";")
	}

	// Trim leading/trailing semicolons and spaces
	joined = strings.Trim(joined, "; ")

	return joined
}

// --- Shared org-scanning / repo-processing ---

// listOrgRepos lists all repos in a GitHub org (paginated).
func listOrgRepos(client *http.Client, token, org string) []ghRepo {
	var all []ghRepo
	page := 1
	for {
		url := fmt.Sprintf("https://api.github.com/orgs/%s/repos?per_page=100&page=%d&type=public", org, page)
		var repos []ghRepo
		if err := ghGet(client, token, url, &repos); err != nil {
			log.Printf("listing repos page %d: %v", page, err)
			break
		}
		if len(repos) == 0 {
			break
		}
		all = append(all, repos...)
		page++
	}
	return all
}

// processRepo tries to find and parse CONF_STR from a repo's top-level .sv files.
func processRepo(client *http.Client, token, cacheDir string, repo ghRepo) *mister.CoreOSD {
	// List top-level files
	url := fmt.Sprintf("https://api.github.com/repos/%s/contents/", repo.FullName)
	var contents []ghContent
	if err := ghGet(client, token, url, &contents); err != nil {
		return nil
	}

	// Find .sv files at root level
	var svFiles []ghContent
	for _, c := range contents {
		if c.Type == "file" && strings.HasSuffix(strings.ToLower(c.Name), ".sv") {
			svFiles = append(svFiles, c)
		}
	}

	if len(svFiles) == 0 {
		// Try hdl/ or rtl/ subdirectory (common in some cores)
		for _, subdir := range []string{"hdl", "rtl", "src"} {
			subURL := fmt.Sprintf("https://api.github.com/repos/%s/contents/%s", repo.FullName, subdir)
			var subContents []ghContent
			if err := ghGet(client, token, subURL, &subContents); err != nil {
				continue
			}
			for _, c := range subContents {
				if c.Type == "file" && strings.HasSuffix(strings.ToLower(c.Name), ".sv") {
					svFiles = append(svFiles, c)
				}
			}
		}
	}

	// Try each .sv file for CONF_STR
	for _, svFile := range svFiles {
		source := downloadFile(client, token, cacheDir, repo.FullName, svFile)
		if source == "" {
			continue
		}
		raw := mister.ExtractConfStr(source)
		if raw == "" {
			continue
		}

		coreName := mister.ExtractCoreName(raw)
		if coreName == "" {
			coreName = mister.RepoToCoreName(repo.Name)
		}

		menu := mister.ParseConfStr(raw)
		return &mister.CoreOSD{
			CoreName:   coreName,
			Repo:       repo.FullName,
			ConfStrRaw: raw,
			Menu:       menu,
		}
	}

	// Also check .v files (plain Verilog)
	var vFiles []ghContent
	for _, c := range contents {
		if c.Type == "file" && strings.HasSuffix(strings.ToLower(c.Name), ".v") {
			vFiles = append(vFiles, c)
		}
	}
	for _, vFile := range vFiles {
		source := downloadFile(client, token, cacheDir, repo.FullName, vFile)
		if source == "" {
			continue
		}
		raw := mister.ExtractConfStr(source)
		if raw == "" {
			continue
		}

		coreName := mister.ExtractCoreName(raw)
		if coreName == "" {
			coreName = mister.RepoToCoreName(repo.Name)
		}

		menu := mister.ParseConfStr(raw)
		return &mister.CoreOSD{
			CoreName:   coreName,
			Repo:       repo.FullName,
			ConfStrRaw: raw,
			Menu:       menu,
		}
	}

	return nil
}

// downloadFile downloads a file, using cache if available.
func downloadFile(client *http.Client, token, cacheDir, repoFullName string, file ghContent) string {
	// Check cache
	if cacheDir != "" {
		cachePath := filepath.Join(cacheDir, strings.ReplaceAll(repoFullName, "/", "_")+"_"+file.Name)
		shaPath := cachePath + ".sha"

		// If cached SHA matches, use cached file
		if cachedSHA, err := os.ReadFile(shaPath); err == nil && strings.TrimSpace(string(cachedSHA)) == file.SHA {
			if data, err := os.ReadFile(cachePath); err == nil {
				return string(data)
			}
		}

		// Download and cache
		source := downloadRaw(client, token, file.DownloadURL)
		if source != "" {
			os.WriteFile(cachePath, []byte(source), 0644)
			os.WriteFile(shaPath, []byte(file.SHA), 0644)
		}
		return source
	}

	return downloadRaw(client, token, file.DownloadURL)
}

// downloadRaw downloads a file by URL.
func downloadRaw(client *http.Client, token, url string) string {
	if url == "" {
		return ""
	}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return ""
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/vnd.github.v3.raw")

	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return ""
	}

	// Limit to 2MB to avoid huge files
	data, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return ""
	}
	return string(data)
}

// ghGet makes an authenticated GitHub API GET request and decodes JSON.
func ghGet(client *http.Client, token, url string, result interface{}) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 403 {
		return fmt.Errorf("rate limited (403) — provide a --token for higher limits")
	}
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("GET %s: status %d: %s", url, resp.StatusCode, string(body))
	}

	return json.NewDecoder(resp.Body).Decode(result)
}
