# gobundler

This tool extends an existing golang tool (https://github.com/golang/tools/blob/master/cmd/bundle/main.go) This tool adds the ability to recursively traverse dependencies and combine them into one file.

## Multi Package bundling

This custom bundler tool is heavily based on the Go bundle tool. We borrowed pieces of functionality from it. The major difference for ours is that our tool will bundle multiple packages together into one package by starting at a root package. It traverses the dependency graph and flattens imports, along with combining all the code from the packages resulting in one file ready for deployment.

## Go Bundle and 2 package bundling

Go bundle is a command-line tool for bundling dependencies https://godoc.org/golang.org/x/tools/cmd/bundle. This allows for creating a single-source-file version of a source package suitable for inclusion in a particular target package.

## How to run the Multi Package Bundle

1. to bundle dependencies of a `targetPackage` run:

```bash
    go run bundle.go
        <targetPackage>
        <targetPackageName>
        [-fileName name]
        [-packageName name]
        [-destinationRoot root]
```

`targetPackage`(required) - this argument represents the fully qualified target package to use as the starting point to combine all of its nested dependencies.

`targetPackageName`(required) - this argument represents the name of the target package

`fileName`(optional) - this argument represents the name of the output file, by default it is plugin.go

`packageName`(optional) - this argument represents the output package name, by default it is main

`destinationRoot`(optional) - this argument represents the destination folder, by default it is build/

2. to invoke validation and tests run:

```bash
$ validate.sh
```
