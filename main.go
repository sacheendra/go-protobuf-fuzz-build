package main

import (
	"flag"
	"go/token"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"strings"
	"text/template"

	"golang.org/x/tools/go/packages"
)

var (
	flagFunc = flag.String("func", "Fuzz", "fuzzer entry point")
	flagO    = flag.String("o", "", "output file")

	flagRace = flag.Bool("race", false, "enable data race detection")
	flagTags = flag.String("tags", "", "a comma-separated list of build tags to consider satisfied during the build")
	flagV    = flag.Bool("v", false, "print the names of packages as they are compiled")
	flagWork = flag.Bool("work", false, "print the name of the temporary work directory and do not remove it when exiting")
	flagX    = flag.Bool("x", false, "print the commands")
)

func main() {
	flag.Parse()

	if !token.IsIdentifier(*flagFunc) || !token.IsExported(*flagFunc) {
		log.Fatal("-func must be an exported identifier")
	}

	tags := "gofuzz,gofuzz_libfuzzer,libfuzzer"
	if *flagTags != "" {
		tags += "," + *flagTags
	}

	buildFlags := []string{
		"-buildmode", "c-archive",
		"-gcflags", "all=-d=libfuzzer",
		"-tags", tags,
		"-trimpath",
	}

	suppress := []string{
		"syscall", // https://github.com/google/oss-fuzz/issues/3639
	}
	for _, pkg := range suppress {
		buildFlags = append(buildFlags, "-gcflags", pkg+"=-d=libfuzzer=0")
	}

	if *flagRace {
		buildFlags = append(buildFlags, "-race")
	}
	if *flagV {
		buildFlags = append(buildFlags, "-v")
	}
	if *flagWork {
		buildFlags = append(buildFlags, "-work")
	}
	if *flagX {
		buildFlags = append(buildFlags, "-x")
	}

	if len(flag.Args()) != 1 {
		log.Fatal("must specify exactly one package path")
	}
	path := flag.Args()[0]
	if strings.Contains(path, "...") {
		log.Fatal("package path must not contain ... wildcards")
	}
	pkgs, err := packages.Load(&packages.Config{
		Mode:       packages.NeedName,
		BuildFlags: buildFlags,
	}, "pattern="+path)
	if err != nil {
		log.Fatal("failed to load packages:", err)
	}
	if packages.PrintErrors(pkgs) != 0 {
		os.Exit(1)
	}
	if len(pkgs) != 1 {
		log.Fatal("package path matched multiple packages")
	}
	pkg := pkgs[0]

	importPath := pkg.PkgPath
	if strings.HasPrefix(importPath, "_/") {
		importPath = path
	}

	mainFile, err := ioutil.TempFile(".", "main.*.go")
	if err != nil {
		log.Fatal("failed to create temporary file:", err)
	}
	defer os.Remove(mainFile.Name())

	type Data struct {
		PkgPath string
		Func    string
	}
	err = mainTmpl.Execute(mainFile, &Data{
		PkgPath: importPath,
		Func:    *flagFunc,
	})
	if err != nil {
		log.Fatal("failed to execute template:", err)
	}
	if err := mainFile.Close(); err != nil {
		log.Fatal(err)
	}

	out := *flagO
	if out == "" {
		out = pkg.Name + "-fuzz.a"
	}

	args := []string{"build", "-o", out}
	args = append(args, buildFlags...)
	args = append(args, mainFile.Name())
	cmd := exec.Command("go", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		log.Fatal("failed to build packages:", err)
	}
}

var mainTmpl = template.Must(template.New("main").Parse(`
// Code generated by go114-fuzz-build; DO NOT EDIT.

// +build ignore

package main

import (
	"unsafe"

	target {{printf "%q" .PkgPath}}
)

// #include <stdint.h>
import "C"

//export LPBMutatorTestOneInput
func LPBMutatorTestOneInput(data *C.char, size C.size_t) C.int {
	// TODO(mdempsky): Use unsafe.Slice once golang.org/issue/19367 is accepted.
	s := (*[1<<30]byte)(unsafe.Pointer(data))[:size:size]
	target.{{.Func}}(s)
	return 0
}

func main() {
}
`))
