//go:build ignore

package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"crypto/sha512"
	"fmt"
	"hash"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ----------------------------------------------------------------------------
// Configuration
// ----------------------------------------------------------------------------

var commands = []string{
	"check",
	"relock",
	"unlock",
	"dumpsmc",
	"hostcaps",
	"patchgos",
	"patchsmc",
	"patchvmkctl",
}

var targets = []struct{ goos, goarch, outDir string }{
	{"windows", "amd64", filepath.Join("build", "windows")},
	{"linux", "amd64", filepath.Join("build", "linux")},
	{"darwin", "amd64", filepath.Join("build", "macos")},
}

// ----------------------------------------------------------------------------
// Main
// ----------------------------------------------------------------------------

func main() {
	task := "build"
	if len(os.Args) > 1 {
		task = os.Args[1]
	}

	switch task {
	case "build":
		runClean()
		runBuild()
		runDist()
	case "clean":
		runClean()
	case "dist":
		runDist()
	case "version":
		fmt.Println(mustReadVersion())
	default:
		fmt.Fprintf(os.Stderr, "Unknown task %q\n\nUsage: go run tasks.go [build|clean|dist|version]\n", task)
		os.Exit(1)
	}
}

// ----------------------------------------------------------------------------
// Tasks
// ----------------------------------------------------------------------------

func runClean() {
	fmt.Println("==> Cleaning build directory")
	must(os.RemoveAll("build"))
	for _, t := range targets {
		must(os.MkdirAll(t.outDir, 0755))
	}
	must(os.MkdirAll(filepath.Join("build", "iso"), 0755))
	must(os.MkdirAll(filepath.Join("build", "templates"), 0755))

	// Remove any stale syso files under ./commands/
	_ = filepath.WalkDir("commands", func(path string, d os.DirEntry, _ error) error {
		if !d.IsDir() && strings.HasSuffix(path, "rsrc_windows_amd64.syso") {
			fmt.Println("Removing", path)
			os.Remove(path)
		}
		return nil
	})
}

func runBuild() {
	version := mustReadVersion()
	fmt.Println("==> Building executables -", version)

	mustWriteVersionGo(version)

	for _, name := range commands {
		dir := filepath.Join("commands", name)
		fmt.Printf("\n-- %s\n", name)

		run(dir, "go-winres", "make",
			"--arch", "amd64",
			"--product-version", version,
			"--file-version", version,
		)

		for _, t := range targets {
			out := filepath.Join("..", "..", t.outDir, exeName(name, t.goos))
			fmt.Printf("   %s/%s -> %s\n", t.goos, t.goarch, out)
			runEnv(dir,
				[]string{"GOOS=" + t.goos, "GOARCH=" + t.goarch},
				"go", "build", "-o", out,
			)
		}

		os.Remove(filepath.Join(dir, "rsrc_windows_amd64.syso"))
	}

	copyAssets()
	fmt.Println("\n==> Done")
}

func runDist() {
	version := mustReadVersion()
	// Strip dots: "1.2.3" -> "123"
	versionFlat := strings.ReplaceAll(version, ".", "")

	must(os.MkdirAll("dist", 0755))

	zipPath := filepath.Join("dist", "unlocker"+versionFlat+".zip")
	tgzPath := filepath.Join("dist", "unlocker"+versionFlat+".tgz")
	dirPath := filepath.Join("dist", "unlocker"+versionFlat)

	fmt.Println("==> Creating distribution files -", version)

	// Remove previous dist artefacts for this version
	os.Remove(zipPath)
	os.Remove(tgzPath)
	os.RemoveAll(dirPath)

	// Create zip and tgz
	fmt.Println("\n-- Creating", zipPath)
	must(createZip(zipPath, "build"))

	fmt.Println("-- Creating", tgzPath)
	must(createTgz(tgzPath, "build"))

	// Extract zip into versioned folder
	fmt.Println("-- Extracting", zipPath, "->", dirPath)
	must(extractZip(zipPath, dirPath))

	// Print checksums
	fmt.Println("\n-- Checksums")
	for _, path := range []string{tgzPath, zipPath} {
		printChecksum(path, sha256.New(), "SHA-256")
		printChecksum(path, sha512.New(), "SHA-512")
	}

	fmt.Println("\n==> Done")
}

// ----------------------------------------------------------------------------
// Archive helpers
// ----------------------------------------------------------------------------

// createZip zips the contents of srcDir (non-recursively at the top level,
// recursively for subdirectories) into destZip, preserving relative paths.
func createZip(destZip, srcDir string) error {
	f, err := os.Create(destZip)
	if err != nil {
		return err
	}
	defer f.Close()

	w := zip.NewWriter(f)
	defer w.Close()

	return filepath.WalkDir(srcDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(srcDir, path)
		// Use forward slashes inside the zip regardless of host OS
		entry, err := w.Create(filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		src, err := os.Open(path)
		if err != nil {
			return err
		}
		defer src.Close()
		fmt.Println("  +", rel)
		_, err = io.Copy(entry, src)
		return err
	})
}

// createTgz tars and gzips the contents of srcDir into destTgz.
func createTgz(destTgz, srcDir string) error {
	f, err := os.Create(destTgz)
	if err != nil {
		return err
	}
	defer f.Close()

	gz := gzip.NewWriter(f)
	defer gz.Close()

	tw := tar.NewWriter(gz)
	defer tw.Close()

	return filepath.WalkDir(srcDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(srcDir, path)

		info, err := d.Info()
		if err != nil {
			return err
		}
		hdr := &tar.Header{
			Name:    filepath.ToSlash(rel),
			Size:    info.Size(),
			Mode:    int64(info.Mode()),
			ModTime: info.ModTime(),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		src, err := os.Open(path)
		if err != nil {
			return err
		}
		defer src.Close()
		fmt.Println("  +", rel)
		_, err = io.Copy(tw, src)
		return err
	})
}

// extractZip extracts a zip archive into destDir.
func extractZip(srcZip, destDir string) error {
	r, err := zip.OpenReader(srcZip)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		outPath := filepath.Join(destDir, filepath.FromSlash(f.Name))
		if f.FileInfo().IsDir() {
			os.MkdirAll(outPath, 0755)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
			return err
		}
		dst, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}
		src, err := f.Open()
		if err != nil {
			dst.Close()
			return err
		}
		_, err = io.Copy(dst, src)
		src.Close()
		dst.Close()
		if err != nil {
			return err
		}
		fmt.Println("  ->", outPath)
	}
	return nil
}

// ----------------------------------------------------------------------------
// Checksum helpers
// ----------------------------------------------------------------------------

func printChecksum(path string, h hash.Hash, label string) {
	f, err := os.Open(path)
	if err != nil {
		fmt.Printf("   Warning: could not open %s: %v\n", path, err)
		return
	}
	defer f.Close()
	if _, err := io.Copy(h, f); err != nil {
		fmt.Printf("   Warning: could not hash %s: %v\n", path, err)
		return
	}
	fmt.Printf("  %s  %s  %x\n", label, path, h.Sum(nil))
}

// ----------------------------------------------------------------------------
// Build helpers
// ----------------------------------------------------------------------------

func mustReadVersion() string {
	data, err := os.ReadFile("VERSION")
	if err != nil {
		fatalf("could not read VERSION file: %v", err)
	}
	return strings.TrimSpace(string(data))
}

func mustWriteVersionGo(version string) {
	content := fmt.Sprintf("package vmwpatch\nconst VERSION = %q\n", version)
	must(os.WriteFile(filepath.Join("vmwpatch", "version.go"), []byte(content), 0644))
}

func exeName(name, goos string) string {
	if goos == "windows" {
		return name + ".exe"
	}
	return name
}

func copyAssets() {
	fmt.Println("\n-- Copying assets")
	copyFile("LICENSE", filepath.Join("build", "LICENSE"))
	copyGlob("*.md", "build")
	copyDirContents(filepath.Join("cpuid", "linux"), filepath.Join("build", "linux"))
	copyDirContents(filepath.Join("cpuid", "windows"), filepath.Join("build", "windows"))
	copyDirContents(filepath.Join("cpuid", "macos"), filepath.Join("build", "macos"))
	copyDirAll("iso", filepath.Join("build", "iso"))
}

func copyFile(src, dst string) {
	data, err := os.ReadFile(src)
	if err != nil {
		fmt.Printf("   Warning: could not read %s: %v\n", src, err)
		return
	}
	if err := os.WriteFile(dst, data, 0755); err != nil {
		fmt.Printf("   Warning: could not write %s: %v\n", dst, err)
		return
	}
	fmt.Printf("   %s -> %s\n", src, dst)
}

func copyGlob(pattern, dstDir string) {
	matches, _ := filepath.Glob(pattern)
	for _, m := range matches {
		copyFile(m, filepath.Join(dstDir, filepath.Base(m)))
	}
}

func copyDirContents(srcDir, dstDir string) {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			copyFile(filepath.Join(srcDir, e.Name()), filepath.Join(dstDir, e.Name()))
		}
	}
}

func copyDirAll(srcDir, dstDir string) {
	_ = filepath.WalkDir(srcDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(srcDir, path)
		dst := filepath.Join(dstDir, rel)
		if d.IsDir() {
			os.MkdirAll(dst, 0755)
		} else {
			copyFile(path, dst)
		}
		return nil
	})
}

func run(dir string, name string, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fatalf("command failed [%s %s]: %v", name, strings.Join(args, " "), err)
	}
}

func runEnv(dir string, env []string, name string, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fatalf("command failed [%s %s]: %v", name, strings.Join(args, " "), err)
	}
}

func must(err error) {
	if err != nil {
		fatalf("%v", err)
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "Error: "+format+"\n", args...)
	os.Exit(1)
}
