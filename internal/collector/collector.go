package collector

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// ──────────────────────────────────────────
// Data types
// ──────────────────────────────────────────

type Report struct {
	Timestamp string        `json:"timestamp"`
	Hostname  string        `json:"hostname"`
	Shell     string        `json:"shell"`
	Cwd       string        `json:"cwd"`
	Tools     []RuntimeTool `json:"tools"`
	Packages  []PackageManager `json:"packages"`
}

type RuntimeTool struct {
	Manager           string   `json:"manager"`
	Language          string   `json:"language"`
	Location          string   `json:"location"`
	ActiveVersion     string   `json:"active_version"`
	GlobalVersion     string   `json:"global_version"`
	LocalVersion      string   `json:"local_version,omitempty"`
	VersionFile       string   `json:"version_file,omitempty"`
	InstalledVersions []string `json:"installed_versions"`
}

type Package struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Latest  string `json:"latest,omitempty"`
	Type    string `json:"type,omitempty"`
}

type PackageManager struct {
	Manager        string    `json:"manager"`
	ManagerVersion string    `json:"manager_version"`
	Language       string    `json:"language"`
	TotalPackages  int       `json:"total_packages"`
	Location       string    `json:"location"`
	Packages       []Package `json:"packages"`
	LocalPackages  []Package `json:"local_packages,omitempty"`
	LocalPkgFile   string    `json:"local_package_file,omitempty"`
}

// ──────────────────────────────────────────
// Collector
// ──────────────────────────────────────────

type Collector struct {
	mu     sync.RWMutex
	report *Report
	watchDirs []string
}

func New() *Collector {
	return &Collector{}
}

// Collect gathers all version and package information
func (c *Collector) Collect() *Report {
	hostname, _ := os.Hostname()
	cwd, _ := os.Getwd()
	shell := filepath.Base(os.Getenv("SHELL"))
	if shell == "" || shell == "." {
		shell = "unknown"
	}

	report := &Report{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Hostname:  hostname,
		Shell:     shell,
		Cwd:       cwd,
		Tools:     []RuntimeTool{},
		Packages:  []PackageManager{},
	}

	// Collect runtimes in parallel
	var wg sync.WaitGroup
	var mu sync.Mutex

	runtimeCollectors := []func() *RuntimeTool{
		c.collectPyenv,
		c.collectGoenv,
		c.collectNvm,
		c.collectFnm,
		c.collectRbenv,
		c.collectJenv,
		c.collectRustup,
		c.collectAsdf,
		c.collectMise,
	}

	for _, fn := range runtimeCollectors {
		wg.Add(1)
		go func(collect func() *RuntimeTool) {
			defer wg.Done()
			if tool := collect(); tool != nil {
				mu.Lock()
				report.Tools = append(report.Tools, *tool)
				mu.Unlock()
			}
		}(fn)
	}

	pkgCollectors := []func() *PackageManager{
		c.collectPip,
		c.collectNpm,
		c.collectGoMod,
		c.collectCargo,
		c.collectGem,
		c.collectBrew,
	}

	for _, fn := range pkgCollectors {
		wg.Add(1)
		go func(collect func() *PackageManager) {
			defer wg.Done()
			if pm := collect(); pm != nil {
				mu.Lock()
				report.Packages = append(report.Packages, *pm)
				mu.Unlock()
			}
		}(fn)
	}

	wg.Wait()

	c.mu.Lock()
	c.report = report
	c.mu.Unlock()

	return report
}

// GetReport returns the latest cached report
func (c *Collector) GetReport() *Report {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.report
}

// GetReportJSON returns the latest report as JSON
func (c *Collector) GetReportJSON() ([]byte, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.report == nil {
		return []byte("{}"), nil
	}
	return json.MarshalIndent(c.report, "", "  ")
}

// WatchDirs returns directories that should be watched for changes
func (c *Collector) WatchDirs() []string {
	cwd, _ := os.Getwd()
	dirs := []string{cwd}
	home := os.Getenv("HOME")
	if home != "" {
		dirs = append(dirs, home)
	}
	return dirs
}

// WatchFiles returns version-related files to watch
func (c *Collector) WatchFiles() []string {
	cwd, _ := os.Getwd()
	home := os.Getenv("HOME")
	files := []string{}

	// Project-level version files
	versionFiles := []string{
		".python-version", ".go-version", ".node-version",
		".ruby-version", ".java-version", ".tool-versions",
		"go.mod", "package.json", "Cargo.toml", "Gemfile",
		"requirements.txt", "pyproject.toml",
	}
	for _, f := range versionFiles {
		full := filepath.Join(cwd, f)
		if _, err := os.Stat(full); err == nil {
			files = append(files, full)
		}
	}

	// Home-level version files
	if home != "" {
		for _, f := range []string{".tool-versions"} {
			full := filepath.Join(home, f)
			if _, err := os.Stat(full); err == nil {
				files = append(files, full)
			}
		}
	}

	return files
}

// ──────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────

func run(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

func which(name string) string {
	path, err := exec.LookPath(name)
	if err != nil {
		return ""
	}
	return path
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func lines(s string) []string {
	result := []string{}
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			result = append(result, line)
		}
	}
	return result
}

// ──────────────────────────────────────────
// Runtime collectors
// ──────────────────────────────────────────

func (c *Collector) collectEnvTool(manager, cmd, lang string, versionsArgs, globalArgs []string) *RuntimeTool {
	if which(cmd) == "" {
		return nil
	}

	installed, _ := run(cmd, versionsArgs...)
	globalVer, _ := run(cmd, globalArgs...)
	location := which(cmd)

	versions := lines(installed)
	if len(versions) == 0 {
		return nil
	}

	tool := &RuntimeTool{
		Manager:           manager,
		Language:          lang,
		Location:          location,
		GlobalVersion:     globalVer,
		ActiveVersion:     globalVer,
		InstalledVersions: versions,
	}

	// Check for local version file
	cwd, _ := os.Getwd()
	localFiles := []string{
		filepath.Join(cwd, "."+lang+"-version"),
		filepath.Join(cwd, ".tool-versions"),
	}
	for _, f := range localFiles {
		if exists(f) {
			if strings.HasSuffix(f, ".tool-versions") {
				data, _ := os.ReadFile(f)
				for _, line := range lines(string(data)) {
					parts := strings.Fields(line)
					if len(parts) >= 2 && parts[0] == lang {
						tool.LocalVersion = parts[1]
						tool.VersionFile = f
						tool.ActiveVersion = parts[1]
						break
					}
				}
			} else {
				data, _ := os.ReadFile(f)
				ver := strings.TrimSpace(string(data))
				if ver != "" {
					tool.LocalVersion = ver
					tool.VersionFile = f
					tool.ActiveVersion = ver
				}
			}
		}
	}

	return tool
}

func (c *Collector) collectPyenv() *RuntimeTool {
	return c.collectEnvTool("pyenv", "pyenv", "python",
		[]string{"versions", "--bare"}, []string{"global"})
}

func (c *Collector) collectGoenv() *RuntimeTool {
	return c.collectEnvTool("goenv", "goenv", "go",
		[]string{"versions", "--bare"}, []string{"global"})
}

func (c *Collector) collectRbenv() *RuntimeTool {
	return c.collectEnvTool("rbenv", "rbenv", "ruby",
		[]string{"versions", "--bare"}, []string{"global"})
}

func (c *Collector) collectJenv() *RuntimeTool {
	return c.collectEnvTool("jenv", "jenv", "java",
		[]string{"versions", "--bare"}, []string{"global"})
}

func (c *Collector) collectFnm() *RuntimeTool {
	return c.collectEnvTool("fnm", "fnm", "node",
		[]string{"ls", "--plain"}, []string{"default"})
}

func (c *Collector) collectNvm() *RuntimeTool {
	nvmDir := os.Getenv("NVM_DIR")
	if nvmDir == "" {
		nvmDir = filepath.Join(os.Getenv("HOME"), ".nvm")
	}
	if !exists(nvmDir) {
		return nil
	}

	// NVM is a shell function, so we need to source it
	script := fmt.Sprintf(`
		export NVM_DIR="%s"
		[ -s "$NVM_DIR/nvm.sh" ] && . "$NVM_DIR/nvm.sh"
		echo "CURRENT=$(nvm current 2>/dev/null)"
		echo "DEFAULT=$(nvm alias default 2>/dev/null | grep -oP 'v[\d.]+' || echo '')"
		nvm ls --no-colors 2>/dev/null | grep -oP 'v[\d.]+' | sort -V | uniq
	`, nvmDir)

	out, err := run("bash", "-c", script)
	if err != nil {
		return nil
	}

	var current, defaultVer string
	var versions []string

	for _, line := range lines(out) {
		if strings.HasPrefix(line, "CURRENT=") {
			current = strings.TrimPrefix(line, "CURRENT=")
		} else if strings.HasPrefix(line, "DEFAULT=") {
			defaultVer = strings.TrimPrefix(line, "DEFAULT=")
		} else if strings.HasPrefix(line, "v") {
			versions = append(versions, line)
		}
	}

	if len(versions) == 0 {
		return nil
	}

	if defaultVer == "" {
		defaultVer = current
	}

	return &RuntimeTool{
		Manager:           "nvm",
		Language:          "node",
		Location:          filepath.Join(nvmDir, "nvm.sh"),
		ActiveVersion:     current,
		GlobalVersion:     defaultVer,
		InstalledVersions: versions,
	}
}

func (c *Collector) collectRustup() *RuntimeTool {
	if which("rustup") == "" {
		return nil
	}

	toolchains, _ := run("rustup", "toolchain", "list")
	defaultTc, _ := run("rustup", "default")

	versions := []string{}
	for _, tc := range lines(toolchains) {
		tc = strings.TrimSuffix(tc, " (default)")
		versions = append(versions, tc)
	}

	if len(versions) == 0 {
		return nil
	}

	defaultVer := strings.Fields(defaultTc)[0]

	return &RuntimeTool{
		Manager:           "rustup",
		Language:          "rust",
		Location:          which("rustup"),
		ActiveVersion:     defaultVer,
		GlobalVersion:     defaultVer,
		InstalledVersions: versions,
	}
}

func (c *Collector) collectAsdf() *RuntimeTool {
	// asdf manages multiple languages, return nil here
	// and handle each plugin separately if needed
	if which("asdf") == "" {
		return nil
	}

	plugins, _ := run("asdf", "plugin", "list")
	// For simplicity, collect each plugin as its own tool
	// This function returns nil; asdf tools are collected separately
	_ = plugins
	return nil
}

func (c *Collector) collectMise() *RuntimeTool {
	if which("mise") == "" {
		return nil
	}
	// mise ls --json gives structured output
	out, err := run("mise", "ls", "--json")
	if err != nil {
		return nil
	}
	_ = out
	// Parse and return - simplified for now
	return nil
}

// ──────────────────────────────────────────
// Package collectors
// ──────────────────────────────────────────

func (c *Collector) collectPip() *PackageManager {
	pipCmd := which("pip3")
	if pipCmd == "" {
		pipCmd = which("pip")
	}
	if pipCmd == "" {
		return nil
	}

	ver, _ := run(pipCmd, "--version")
	parts := strings.Fields(ver)
	pipVer := "unknown"
	if len(parts) >= 2 {
		pipVer = parts[1]
	}

	// Get installed packages
	out, err := run(pipCmd, "list", "--format=json")
	if err != nil {
		return nil
	}

	var installed []struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	json.Unmarshal([]byte(out), &installed)

	// Get outdated
	outdatedOut, _ := run(pipCmd, "list", "--outdated", "--format=json")
	var outdated []struct {
		Name    string `json:"name"`
		Latest  string `json:"latest_version"`
	}
	json.Unmarshal([]byte(outdatedOut), &outdated)
	outdatedMap := map[string]string{}
	for _, o := range outdated {
		outdatedMap[o.Name] = o.Latest
	}

	pkgs := make([]Package, 0, len(installed))
	for _, p := range installed {
		pkg := Package{Name: p.Name, Version: p.Version}
		if latest, ok := outdatedMap[p.Name]; ok {
			pkg.Latest = latest
		}
		pkgs = append(pkgs, pkg)
	}

	return &PackageManager{
		Manager:        "pip",
		ManagerVersion: pipVer,
		Language:       "python",
		TotalPackages:  len(pkgs),
		Location:       pipCmd,
		Packages:       pkgs,
	}
}

func (c *Collector) collectNpm() *PackageManager {
	if which("npm") == "" {
		return nil
	}

	ver, _ := run("npm", "--version")

	// Global packages
	out, _ := run("npm", "ls", "-g", "--depth=0", "--json")
	var npmData struct {
		Dependencies map[string]struct {
			Version string `json:"version"`
		} `json:"dependencies"`
	}
	json.Unmarshal([]byte(out), &npmData)

	pkgs := []Package{}
	for name, info := range npmData.Dependencies {
		pkgs = append(pkgs, Package{Name: name, Version: info.Version})
	}

	pm := &PackageManager{
		Manager:        "npm",
		ManagerVersion: ver,
		Language:       "node",
		TotalPackages:  len(pkgs),
		Location:       which("npm"),
		Packages:       pkgs,
	}

	// Local packages from package.json
	cwd, _ := os.Getwd()
	pkgJsonPath := filepath.Join(cwd, "package.json")
	if exists(pkgJsonPath) {
		data, _ := os.ReadFile(pkgJsonPath)
		var pkgJson struct {
			Dependencies    map[string]string `json:"dependencies"`
			DevDependencies map[string]string `json:"devDependencies"`
		}
		json.Unmarshal(data, &pkgJson)

		var localPkgs []Package
		for name, ver := range pkgJson.Dependencies {
			localPkgs = append(localPkgs, Package{Name: name, Version: ver, Type: "prod"})
		}
		for name, ver := range pkgJson.DevDependencies {
			localPkgs = append(localPkgs, Package{Name: name, Version: ver, Type: "dev"})
		}
		if len(localPkgs) > 0 {
			pm.LocalPackages = localPkgs
			pm.LocalPkgFile = pkgJsonPath
		}
	}

	return pm
}

func (c *Collector) collectGoMod() *PackageManager {
	if which("go") == "" {
		return nil
	}

	verOut, _ := run("go", "version")
	goVer := "unknown"
	if parts := strings.Fields(verOut); len(parts) >= 3 {
		goVer = strings.TrimPrefix(parts[2], "go")
	}

	cwd, _ := os.Getwd()
	modPath := filepath.Join(cwd, "go.mod")
	if !exists(modPath) {
		return &PackageManager{
			Manager:        "go modules",
			ManagerVersion: goVer,
			Language:       "go",
			TotalPackages:  0,
			Location:       which("go"),
			Packages:       []Package{},
		}
	}

	data, _ := os.ReadFile(modPath)
	content := string(data)

	pkgs := []Package{}
	requireBlock := regexp.MustCompile(`require\s*\(([\s\S]*?)\)`)
	matches := requireBlock.FindAllStringSubmatch(content, -1)
	for _, match := range matches {
		for _, line := range lines(match[1]) {
			if strings.HasPrefix(line, "//") {
				continue
			}
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				typ := "direct"
				if strings.Contains(line, "// indirect") {
					typ = "indirect"
				}
				pkgs = append(pkgs, Package{
					Name: parts[0], Version: parts[1], Type: typ,
				})
			}
		}
	}

	return &PackageManager{
		Manager:        "go modules",
		ManagerVersion: goVer,
		Language:       "go",
		TotalPackages:  len(pkgs),
		Location:       which("go"),
		Packages:       pkgs,
		LocalPkgFile:   modPath,
	}
}

func (c *Collector) collectCargo() *PackageManager {
	if which("cargo") == "" {
		return nil
	}

	verOut, _ := run("cargo", "--version")
	cargoVer := "unknown"
	if parts := strings.Fields(verOut); len(parts) >= 2 {
		cargoVer = parts[1]
	}

	// Global binaries
	home := os.Getenv("HOME")
	binDir := filepath.Join(home, ".cargo", "bin")
	pkgs := []Package{}
	skip := map[string]bool{"cargo": true, "rustc": true, "rustup": true, "rustfmt": true, "cargo-fmt": true, "cargo-clippy": true, "clippy-driver": true}

	if exists(binDir) {
		entries, _ := os.ReadDir(binDir)
		for _, e := range entries {
			if !skip[e.Name()] {
				pkgs = append(pkgs, Package{Name: e.Name(), Version: "installed", Type: "binary"})
			}
		}
	}

	pm := &PackageManager{
		Manager:        "cargo",
		ManagerVersion: cargoVer,
		Language:       "rust",
		TotalPackages:  len(pkgs),
		Location:       which("cargo"),
		Packages:       pkgs,
	}

	// Local Cargo.toml
	cwd, _ := os.Getwd()
	cargoToml := filepath.Join(cwd, "Cargo.toml")
	if exists(cargoToml) {
		data, _ := os.ReadFile(cargoToml)
		var localPkgs []Package
		inDeps, inDev := false, false
		for _, line := range lines(string(data)) {
			switch {
			case line == "[dependencies]":
				inDeps, inDev = true, false
			case line == "[dev-dependencies]":
				inDeps, inDev = false, true
			case strings.HasPrefix(line, "["):
				inDeps, inDev = false, false
			case (inDeps || inDev) && strings.Contains(line, "="):
				parts := strings.SplitN(line, "=", 2)
				name := strings.TrimSpace(parts[0])
				ver := strings.Trim(strings.TrimSpace(parts[1]), "\"'")
				typ := "prod"
				if inDev {
					typ = "dev"
				}
				localPkgs = append(localPkgs, Package{Name: name, Version: ver, Type: typ})
			}
		}
		if len(localPkgs) > 0 {
			pm.LocalPackages = localPkgs
			pm.LocalPkgFile = cargoToml
		}
	}

	return pm
}

func (c *Collector) collectGem() *PackageManager {
	if which("gem") == "" {
		return nil
	}

	ver, _ := run("gem", "--version")
	out, _ := run("gem", "list", "--no-versions")

	pkgs := []Package{}
	for _, name := range lines(out) {
		pkgs = append(pkgs, Package{Name: name, Version: "installed"})
	}

	return &PackageManager{
		Manager:        "gem",
		ManagerVersion: ver,
		Language:       "ruby",
		TotalPackages:  len(pkgs),
		Location:       which("gem"),
		Packages:       pkgs,
	}
}

func (c *Collector) collectBrew() *PackageManager {
	if which("brew") == "" {
		return nil
	}

	verOut, _ := run("brew", "--version")
	brewVer := "unknown"
	if parts := strings.Fields(verOut); len(parts) >= 2 {
		brewVer = parts[1]
	}

	out, _ := run("brew", "list", "--versions")
	pkgs := []Package{}
	for _, line := range lines(out) {
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			pkgs = append(pkgs, Package{Name: parts[0], Version: parts[len(parts)-1]})
		}
	}

	return &PackageManager{
		Manager:        "brew",
		ManagerVersion: brewVer,
		Language:       "system",
		TotalPackages:  len(pkgs),
		Location:       which("brew"),
		Packages:       pkgs,
	}
}
