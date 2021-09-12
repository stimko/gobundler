#!/bin/bash

set -e
declare -r currentDirectory=$(pwd)
declare -r type=$1

#validate
cd $currentDirectory/build/$type
go build -o validate ../../tb/odndplugin/cmdline-tools/validate/main.go && ./validate -src plugin.go -verbose Console

#copy plugin to test folder and run tests
cp $currentDirectory/build/$type/plugin.go $currentDirectory/test/$type/plugin.go
cd $currentDirectory/test/$type
go test -v -cover

#get rid of use of unsafe package and still need to do some manual after this
#sed -i '' 's/(\*\[2\]uint64)(unsafe\.Pointer(\&p\[i\*16\]))/binary\.LittleEndian\.Uint64(p\[i\*16\:\])/g' $currentDirectory/build/$type/plugin.go
#sed -i '' 's/\*(\*uint32)(unsafe\.Pointer(\&p\[i\*4\]))/binary\.LittleEndian\.Uint32(p\[i\*4\:\])/g' $currentDirectory/build/$type/plugin.go
#sed -i '' '/"unsafe"/d' $currentDirectory/build/$type/plugin.go