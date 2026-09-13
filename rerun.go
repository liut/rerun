// Copyright 2013 The rerun AUTHORS. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"
)

var (
	do_tests  = flag.Bool("test", false, "Run tests (before running program)")
	do_build  = flag.Bool("build", false, "Build program")
	ignore    = flag.String("ignore", "", "ignore by special pattern")
	no_git    = flag.Bool("no-git", true, "ignore .git directory")
	watch     = flag.String("watch", "", "root directory to watch")
	goexec    = flag.String("goexec", "", "bin directory of go")
	rundir    = flag.String("rundir", ".", "bin direcotry for run")
	trimpath  = flag.Bool("trimpath", false, "remove all file system paths from the resulting binary")
)

type scanCallback func(dir string)

func scanChanges(dir string, cb scanCallback) {
	log("watching: %s", dir)

	last := time.Now()

	for {
		walkErr := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if *no_git && info.IsDir() && p == filepath.Join(dir, ".git") {
				return filepath.SkipDir
			}
			if *ignore != "" {
				match, _ := path.Match(*ignore, path.Base(p))
				if match {
					return filepath.SkipDir
				}
			}

			if info.ModTime().After(last) {
				cb(dir)
				last = time.Now()
			}
			return nil
		})
		if walkErr != nil {
			log("watch error: %s", walkErr)
		}

		time.Sleep(500 * time.Millisecond)
	}
}

func log(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "[rerun] %s", fmt.Sprintf(format+"\n", args...))
}

func gobuild(buildpath string) (bool, error) {
	args := []string{"build"}
	if len(*rundir) > 1 {
		args = append(args, "-o", *rundir+"/")
	}
	if *trimpath {
		args = append(args, "-trimpath")
	}
	args = append(args, "-v", buildpath)
	name := "go"
	if len(*goexec) > 2 {
		name = path.Join(*goexec, "go")
	}
	cmd := exec.Command(name, args...)

	buf := bytes.NewBuffer([]byte{})
	cmd.Stdout = buf
	cmd.Stderr = buf

	if err := cmd.Run(); err != nil {
		log("build failed: %s", buf.String())
		return false, err
	}

	log("build succeeded")
	return true, nil
}

func gotest(buildpath string) (bool, error) {
	args := []string{"test", "-v"}
	if *trimpath {
		args = append(args, "-trimpath")
	}
	args = append(args, buildpath)
	cmd := exec.Command(*goexec+"go", args...)

	buf := bytes.NewBuffer([]byte{})
	cmd.Stdout = buf
	cmd.Stderr = buf

	if err := cmd.Run(); err != nil {
		log("tests failed")
		fmt.Println(buf.String())
		return false, err
	}

	log("tests passed")
	return true, nil
}

func run(ch chan bool, bin string, args []string) {
	go func() {
		var proc *os.Process

		for relaunch := range ch {
			if proc != nil {
				if err := proc.Signal(os.Interrupt); err != nil {
					if kerr := proc.Kill(); kerr != nil {
						log("kill error: %s", kerr)
					}
				}
				if _, werr := proc.Wait(); werr != nil {
					log("wait error: %s", werr)
				}
			}

			if !relaunch {
				continue
			}

			log("start run %s", bin)
			cmd := exec.Command(bin, args...)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr

			if err := cmd.Start(); err != nil {
				log("error: %s", err)
			}

			proc = cmd.Process
		}
	}()
}

func refresh(buildpath string, ch chan bool) {
	if *do_tests {
		if ok, _ := gotest(buildpath); !ok {
			ch <- false
			return
		}
	}

	if *do_build {
		if ok, _ := gobuild(buildpath); !ok {
			ch <- false
			return
		}
	}

	// if ok, _ := install(buildpath); !ok {
	// 	ch <- false
	// 	return
	// }

	ch <- true
}

func rerun(buildpath string, args []string) (err error) {
	var bin string
	ch := make(chan bool)

	// pkg, err := build.Import(buildpath, "", 0)
	// if err != nil {
	// 	return err
	// }

	// if pkg.Name != "main" {
	// 	err = errors.New(fmt.Sprintf("expected package %q, got %q", "main", pkg.Name))
	// 	return err
	// }
	_, name := path.Split(buildpath)
	if len(name) == 0 || name == "." {
		if module, err := getModule(buildpath); err == nil {
			_, name = path.Split(module)
		}
	}
	if len(name) == 0 {
		name = "app"
	}

	bin = filepath.Join(*rundir, name)
	if bin == name { // current direcotry
		bin = "./" + bin
	}

	go run(ch, bin, args)

	refresh(buildpath, ch)

	// dir, err := buildpathDir(buildpath)
	// if err != nil {
	// 	return
	// }
	dir := buildpath

	// watch alternate dir
	if watch != nil && *watch != "" {
		dir = *watch
	}

	scanChanges(dir, func(path string) {
		log("change detected")
		refresh(buildpath, ch)
	})

	return
}

func main() {
	flag.Parse()

	if len(flag.Args()) < 1 {
		fmt.Println("Usage: rerun [--no-git] [--test] [--no-run] [--build] [--trimpath] [--race] <import path> [arg]*")
		os.Exit(1)
	}

	if *ignore != "" {
		log("ignoring '%s' dir", *ignore)
	}

	if *no_git {
		log("ignoring .git dir")
	}

	if *goexec != "" {
		if _, err := os.Stat(*goexec + "/go"); os.IsNotExist(err) {
			fmt.Println("invalid path " + *goexec)
			os.Exit(1)
		}
		log("use goexec %s", *goexec)
		*goexec += "/"
	}

	buildpath := flag.Args()[0]
	args := flag.Args()[1:]

	if err := rerun(buildpath, args); err != nil {
		log("error: %s", err)
	}
}

func getModule(dir string) (module string, err error) {
	f, err := os.Open(path.Join(dir, "go.mod"))
	if err != nil {
		return
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()
	scanner := bufio.NewScanner(f)
	scanner.Scan()
	if a, b, ok := strings.Cut(scanner.Text(), " "); ok && a == "module" {
		module = b
	}
	err = scanner.Err()
	return
}
