// Command sherpa packs sherpa-onnx's libraries, which write podcasts'
// captions on the device, for the app to download with the model: each
// platform's, signed on macOS, and Silero's voice activity detector, which
// every platform shares. It writes them, gzipped, into a directory to
// upload as the assets of the release internal/speech downloads them from,
// and internal/speech/natives.go, which names them with their digests:
//
//	go run ./scripts/sherpa -sign - -out build/sherpa
//	go run ./scripts/sherpa -sign "Developer ID Application: …" -out build/sherpa
//	go run ./scripts/sherpa -platforms windows-amd64,linux-amd64 -out build/sherpa
//	gh release create sherpa-onnx-2.0.1 --prerelease --latest=false build/sherpa/*
//
// Packing macOS needs a Mac. Use -sign - for an ad hoc signed app, or the
// app's Developer ID when its hardened runtime enables library validation.
//
// The upstream archives are checked against their digests, and kept in the
// user's cache between runs.
package main

import (
	"archive/tar"
	"bytes"
	"compress/bzip2"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// version is the sherpa-onnx the app is written for: internal/speech
// mirrors the structs of its C API.
const version = "2.0.1"

// release is the tag of the app's release the files are uploaded to.
const release = "sherpa-onnx-" + version

const repo = "zhangnjsd/mygo-music"

// An archive of sherpa-onnx's release, and the libraries the app takes
// from it, the C API's first.
type archive struct {
	name, sha256 string
	files        []string
}

var archives = map[string]archive{
	"darwin-arm64": {"osx-arm64-shared-no-tts-lib", "ac5594ad237db56ec4a6c4b3fc48700441b33a07bb71c235f35530a52a93f2e3",
		[]string{"libsherpa-onnx-c-api.dylib", "libonnxruntime.dylib"}},
	"darwin-amd64": {"osx-x64-shared-no-tts-lib", "b0107b8166c4131245380bc9b984345c4ea8637c65807f09ca5f501c72926be5",
		[]string{"libsherpa-onnx-c-api.dylib", "libonnxruntime.dylib"}},
	"linux-amd64": {"linux-x64-shared-no-tts-lib", "ce437357915491e6f2754b52bec7dcf62ff6fc6aaed9cdcd66f17807bbaef374",
		[]string{"libsherpa-onnx-c-api.so", "libonnxruntime.so"}},
	"linux-arm64": {"linux-aarch64-shared-cpu-lib", "15bd737eae467da32ec97e3740a329a535fd21e33caca4f327b0081451d11533",
		[]string{"libsherpa-onnx-c-api.so", "libonnxruntime.so"}},
	// Linked statically to the C runtime (MT): no Visual C++
	// Redistributable to install.
	"windows-amd64": {"win-x64-shared-MT-Release-no-tts-lib", "21da6a8b92d63ca02ad41dbfc120012343aaaec2e5e02b14819c6b4f38004509",
		[]string{"sherpa-onnx-c-api.dll", "onnxruntime.dll", "onnxruntime_providers_shared.dll"}},
	"windows-arm64": {"win-arm64-shared-MT-Release-no-tts-lib", "b3099bec60e6c3958275e90d0803d837cfe3ad6ed08528f29505d713b3a3e1bb",
		[]string{"sherpa-onnx-c-api.dll", "onnxruntime.dll", "onnxruntime_providers_shared.dll"}},
}

// The voice activity detector, Silero's, as sherpa-onnx publishes it.
const (
	vadURL    = "https://github.com/k2-fsa/sherpa-onnx/releases/download/asr-models/silero_vad.onnx"
	vadSHA256 = "9e2449e1087496d8d4caba907f23e0bd3f78d91fa552479bb9c23ac09cbb1fd6"
)

func main() {
	sign := flag.String("sign", "", "the macOS signing identity ('-' for ad hoc)")
	out := flag.String("out", "build/sherpa", "where to write the files to upload")
	names := flag.String("platforms", all(), "the platforms to pack, of "+all())
	flag.Parse()
	platforms, err := pick(*names)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sherpa:", err)
		os.Exit(2)
	}
	if *sign == "" && slices.ContainsFunc(platforms, func(p string) bool { return strings.HasPrefix(p, "darwin-") }) {
		fmt.Fprintln(os.Stderr, "sherpa: -sign is required for macOS libraries: use '-' for an ad hoc app, or the app's Developer ID for library validation")
		os.Exit(2)
	}
	if err := run(*sign, *out, platforms); err != nil {
		fmt.Fprintln(os.Stderr, "sherpa:", err)
		os.Exit(1)
	}
}

// all returns every platform the archives have, comma separated.
func all() string {
	names := make([]string, 0, len(archives))
	for p := range archives {
		names = append(names, p)
	}
	slices.Sort(names)
	return strings.Join(names, ",")
}

// pick returns the platforms of names, comma separated, sorted, where each
// one is one of the archives'.
func pick(names string) ([]string, error) {
	platforms := strings.Split(names, ",")
	for _, p := range platforms {
		if _, ok := archives[p]; !ok {
			return nil, fmt.Errorf("no platform %q, of %s", p, all())
		}
	}
	slices.Sort(platforms)
	return platforms, nil
}

// A packed file: its asset, and the file it unpacks to.
type packed struct {
	name, asset      string
	size, gzSize     int64
	sha256, gzSHA256 string
}

func run(identity, out string, platforms []string) error {
	if err := os.RemoveAll(out); err != nil {
		return err
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return err
	}
	vad, err := fetch(cache, vadURL, vadSHA256)
	if err != nil {
		return err
	}
	b, err := os.ReadFile(vad)
	if err != nil {
		return err
	}
	vadFile, err := pack(out, "silero_vad.onnx", "silero_vad.onnx.gz", b)
	if err != nil {
		return err
	}
	libs := map[string][]packed{}
	for _, p := range platforms {
		a := archives[p]
		url := fmt.Sprintf("https://github.com/k2-fsa/sherpa-onnx/releases/download/v%s/sherpa-onnx-v%s-%s.tar.bz2", version, version, a.name)
		file, err := fetch(cache, url, a.sha256)
		if err != nil {
			return err
		}
		libs[p], err = packPlatform(out, identity, p, file, a.files)
		if err != nil {
			return err
		}
		fmt.Printf("packed sherpa-onnx %s for %s\n", version, p)
	}
	return writeManifest(filepath.Join("internal", "speech", "natives.go"), platforms, libs, vadFile)
}

// Extract at most one platform at a time, including when packaging fails.
func packPlatform(out, identity, platform, archive string, names []string) (_ []packed, err error) {
	tmp, err := os.MkdirTemp("", "sherpa-"+platform)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, os.RemoveAll(tmp)) }()
	if err := extract(archive, tmp, names); err != nil {
		return nil, err
	}
	files := make([]packed, 0, len(names))
	for _, name := range names {
		lib := filepath.Join(tmp, name)
		if strings.HasPrefix(platform, "darwin-") {
			cmd := exec.Command("codesign", "--force", "--sign", identity, "--options", "runtime", "--timestamp", lib)
			if b, err := cmd.CombinedOutput(); err != nil {
				return nil, fmt.Errorf("codesign %s/%s: %v\n%s", platform, name, err, b)
			}
		}
		b, err := os.ReadFile(lib)
		if err != nil {
			return nil, err
		}
		f, err := pack(out, name, platform+"-"+name+".gz", b)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, nil
}

// pack gzips b into out as asset.
func pack(out, name, asset string, b []byte) (packed, error) {
	var gz bytes.Buffer
	w, _ := gzip.NewWriterLevel(&gz, gzip.BestCompression)
	w.Write(b)
	if err := w.Close(); err != nil {
		return packed{}, err
	}
	if err := os.WriteFile(filepath.Join(out, asset), gz.Bytes(), 0o644); err != nil {
		return packed{}, err
	}
	sum, gzSum := sha256.Sum256(b), sha256.Sum256(gz.Bytes())
	return packed{name: name, asset: asset, size: int64(len(b)), gzSize: int64(gz.Len()),
		sha256: hex.EncodeToString(sum[:]), gzSHA256: hex.EncodeToString(gzSum[:])}, nil
}

func writeManifest(file string, platforms []string, libs map[string][]packed, vad packed) error {
	var b strings.Builder
	lit := func(f packed) string {
		return fmt.Sprintf("{name: %q, asset: %q, size: %d, gzSize: %d, sha256: %q, gzSHA256: %q}", f.name, f.asset, f.size, f.gzSize, f.sha256, f.gzSHA256)
	}
	files := make(map[string]string, len(platforms))
	for _, p := range platforms {
		var entries strings.Builder
		for _, f := range libs[p] {
			fmt.Fprintf(&entries, "%s,\n", lit(f))
		}
		files[p] = entries.String()
	}
	src := []byte(fmt.Sprintf("// Code generated by go run ./scripts/sherpa; DO NOT EDIT.\n\npackage speech\n\n// libVersion is the sherpa-onnx whose libraries the app downloads.\nconst libVersion = %q\n\n// libBase is where they download from: the assets of a release of the app.\nconst libBase = %q\n\n", version, "https://github.com/"+repo+"/releases/download/"+release+"/"))
	if old, err := os.ReadFile(file); err == nil {
		existing, err := existingManifest(old)
		if err != nil {
			return err
		}
		for p, entries := range existing {
			if _, selected := files[p]; !selected {
				files[p] = entries
			}
		}
		other := otherDeclarations(old)
		if len(other) > 0 {
			src = append(src, other...)
			src = append(src, '\n', '\n')
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	b.Write(src)
	fmt.Fprintf(&b, "// libFiles are the libraries of each platform, the C API's first.\nvar libFiles = map[string][]nativeFile{\n")
	names := make([]string, 0, len(files))
	for p := range files {
		names = append(names, p)
	}
	slices.Sort(names)
	for _, p := range names {
		fmt.Fprintf(&b, "\t%q: {\n", p)
		for _, line := range strings.Split(strings.TrimSuffix(files[p], "\n"), "\n") {
			fmt.Fprintf(&b, "\t\t%s\n", line)
		}
		fmt.Fprintf(&b, "\t},\n")
	}
	fmt.Fprintf(&b, "}\n\n// vadFile is Silero's voice activity detector.\nvar vadFile = nativeFile%s\n", lit(vad))
	formatted, err := format.Source([]byte(b.String()))
	if err != nil {
		return err
	}
	return os.WriteFile(file, formatted, 0o644)
}

// existingManifest returns the file entries of libFiles, formatted as nativeFile literals.
func existingManifest(src []byte) (map[string]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "natives.go", src, 0)
	if err != nil {
		return nil, err
	}
	files := map[string]string{}
	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			continue
		}
		for _, spec := range gen.Specs {
			value := spec.(*ast.ValueSpec)
			if len(value.Names) != 1 || value.Names[0].Name != "libFiles" || len(value.Values) != 1 {
				continue
			}
			literal, ok := value.Values[0].(*ast.CompositeLit)
			if !ok {
				return nil, errors.New("libFiles is not a composite literal")
			}
			for _, element := range literal.Elts {
				entry := element.(*ast.KeyValueExpr)
				platform, err := strconv.Unquote(entry.Key.(*ast.BasicLit).Value)
				if err != nil {
					return nil, err
				}
				var entries strings.Builder
				for _, file := range entry.Value.(*ast.CompositeLit).Elts {
					entries.Write(src[fset.Position(file.Pos()).Offset:fset.Position(file.End()).Offset])
					entries.WriteString(",\n")
				}
				files[platform] = entries.String()
			}
			return files, nil
		}
	}
	return files, nil
}

// otherDeclarations returns declarations that are not generated by writeManifest.
func otherDeclarations(src []byte) []byte {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "natives.go", src, parser.ParseComments)
	if err != nil {
		return nil
	}
	var b strings.Builder
	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || (gen.Tok == token.CONST && len(gen.Specs) == 1 && (gen.Specs[0].(*ast.ValueSpec).Names[0].Name == "libVersion" || gen.Specs[0].(*ast.ValueSpec).Names[0].Name == "libBase")) || (gen.Tok == token.VAR && len(gen.Specs) == 1 && (gen.Specs[0].(*ast.ValueSpec).Names[0].Name == "libFiles" || gen.Specs[0].(*ast.ValueSpec).Names[0].Name == "vadFile")) {
			continue
		}
		start := fset.Position(decl.Pos()).Offset
		if gen.Doc != nil {
			start = fset.Position(gen.Doc.Pos()).Offset
		}
		b.Write(src[start:fset.Position(decl.End()).Offset])
		b.WriteString("\n\n")
	}
	return []byte(strings.TrimSpace(b.String()))
}

// fetch returns the path of url's file in the user's cache, downloading it
// there first, unless it is there already: what has sha256 as its digest.
func fetch(cache, url, sum string) (_ string, err error) {
	file := filepath.Join(cache, "mygo-cloudmusic", "sherpa-onnx", sum, path.Base(url))
	if digest(file) == sum {
		return file, nil
	}
	fmt.Println("downloading", url)
	res, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s: %s", url, res.Status)
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return "", err
	}
	out, err := os.CreateTemp(filepath.Dir(file), ".download-*.part")
	if err != nil {
		return "", err
	}
	part := out.Name()
	defer func() {
		if err != nil {
			if removeErr := os.Remove(part); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				err = errors.Join(err, removeErr)
			}
		}
	}()
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(out, h), res.Body)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != sum {
		return "", fmt.Errorf("%s: digest %s, want %s", url, got, sum)
	}
	return file, os.Rename(part, file)
}

func digest(file string) string {
	f, err := os.Open(file)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

// extract writes the files of the archive named, wherever in it they are,
// into dir.
func extract(archive, dir string, names []string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	want := map[string]bool{}
	for _, n := range names {
		want[n] = true
	}
	tr := tar.NewReader(bzip2.NewReader(f))
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		name := path.Base(h.Name)
		if h.Typeflag != tar.TypeReg || !want[name] {
			continue
		}
		out, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
		if err != nil {
			return err
		}
		_, err = io.Copy(out, tr)
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
		delete(want, name)
	}
	if len(want) > 0 {
		return fmt.Errorf("%s has no %v", filepath.Base(archive), want)
	}
	return nil
}
