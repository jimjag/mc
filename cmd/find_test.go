/*
 * Minio Client (C) 2017 Minio, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package cmd

import (
	"testing"
)

//TestFind is the structure used to contain params pertinent to find related tests
type TestFind struct {
	pattern, filePath, flagName string
	match                       bool
}

var basicTests = []TestFind{
	//basic name and path tests
	{"*.jpg", "carter.jpg", "name", true},
	{"*.jpg", "carter.jpeg", "name", false},
	{"*/test/*", "/test/bob/likes/cake", "name", false},
	{"*/test/*", "/test/bob/likes/cake", "path", true},
	{"*test/*", "bob/test/likes/cake", "name", false},
	{"*/test/*", "bob/test/likes/cake", "path", true},
	{"*test/*", "bob/likes/test/cake", "name", false},

	//more advanced name and path tests
	{"*/test/*", "bob/likes/cake/test", "name", false},
	{"*.jpg", ".jpg/elves/are/evil", "name", false},
	{"*.jpg", ".jpg/elves/are/evil", "path", false},
	{"*/test/*", "test1/test2/test3/test", "path", false},
	{"*/ test /*", "test/test1/test2/test3/test", "path", false},
	{"*/test/*", " test /I/have/Really/Long/hair", "path", false},
	{"*XA==", "I/enjoy/morning/walks/XA==", "name ", true},
	{"*XA==", "XA==/Height/is/a/social/construct", "path", false},
	{"*W", "/Word//this/is a/trickyTest", "path", false},
	{"*parser", "/This/might/mess up./the/parser", "name", true},
	{"*", "/bla/bla/bla/ ", "name", true},
	{"*LTIxNDc0ODM2NDgvLTE=", "What/A/Naughty/String/LTIxNDc0ODM2NDgvLTE=", "name", true},
	{"LTIxNDc0ODM2NDgvLTE=", "LTIxNDc0ODM2NDgvLTE=/I/Am/One/Baaaaad/String", "path", false},
	{"wq3YgNiB2ILYg9iE2IXYnNud3I/hoI7igIvigIzigI3igI7igI/igKrigKvigKzigK3igK7igaDi", "An/Even/Bigger/String/wq3YgNiB2ILYg9iE2IXYnNud3I/hoI7igIvigIzigI3igI7igI/igKrigKvigKzigK3igK7igaDi", "name", false},
	{"/", "funky/path/name", "path", false},
	{"𝕿𝖍𝖊", "well/this/isAN/odd/font/THE", "name", false},
	{"𝕿𝖍𝖊", "well/this/isAN/odd/font/The", "name", false},
	{"𝕿𝖍𝖊", "well/this/isAN/odd/font/𝓣𝓱𝓮", "name", false},
	{"𝕿𝖍𝖊", "what/a/strange/turn/of/events/𝓣he", "name", false},
	{"𝕿𝖍𝖊", "well/this/isAN/odd/font/𝕿𝖍𝖊", "name", true},

	//implement some tests of regex

}

func TestFindMethod(t *testing.T) {
	for _, test := range basicTests {
		switch test.flagName {
		case "name":
			if testMatch, _ := nameMatch(test.filePath, test.pattern); testMatch != test.match {
				t.Fatalf("Unexpected result %t, with pattern %s, flag %s  and filepath %s \n", !test.match, test.pattern, test.flagName, test.filePath)
			}
		case "path":
			if testMatch := pathMatch(test.filePath, test.pattern); testMatch != test.match {
				t.Fatalf("Unexpected result %t, with pattern %s, flag %s and filepath %s \n", !test.match, test.pattern, test.flagName, test.filePath)
			}

		}
	}
}
