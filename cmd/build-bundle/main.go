package main

import (
	"flag"
	"fmt"
)

func main() {
	spec := flag.String("spec", "bundle.yaml", "path to the bundle spec file")
	lock := flag.String("lock", "bundle.lock.json", "path to the lock file")
	output := flag.String("output", "dist/", "output directory for the bundle")

	flag.Parse()

	fmt.Printf("build-bundle: not implemented (spec=%s, lock=%s, output=%s)\n", *spec, *lock, *output)
}
