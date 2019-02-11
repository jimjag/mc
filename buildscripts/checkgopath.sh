#!/usr/bin/env bash
#
# Minio Client, (C) 2015, 2016, 2017, 2018, 2019 Minio, Inc.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#

# shellcheck source=buildscripts/build.env
. "$(pwd)/buildscripts/build.env"

main() {
    installed_go_version=$(go version | sed 's/^.* go\([0-9.]*\).*$/\1/')
    # this check is only needed for < go1.11
    if ! check_minimum_version "1.11" "${installed_go_version}"; then
        gopath=$(go env GOPATH)
        IFS=':' read -r -a paths <<< "$gopath"
        for path in "${paths[@]}"; do
            mcpath="$path/src/github.com/minio/mc"
            if [ -d "$mcpath" ]; then
                if [ "$mcpath" -ef "$PWD" ]; then
                    exit 0
                fi
            fi
        done

        echo "Project not found in ${gopath}. Follow instructions at https://github.com/minio/mc/blob/master/CONTRIBUTING.md#setup-your-mc-github-repository"
        exit 1
    fi
}

main
