// +build codegen

// Copyright 2014-2016 Amazon.com, Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//	http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

// Note: This package uses the 'codegen' directive for compatibility with
// the  AWS SDK's code generation scripts.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/aws/aws-sdk-go/private/model/api"
	"github.com/aws/aws-sdk-go/private/util"
	"golang.org/x/tools/imports"
)

func main() {
	os.Exit(_main())
}

func ShapeListWithErrors(a *api.API) []*api.Shape {
	list := make([]*api.Shape, 0, len(a.Shapes))
	for _, n := range a.ShapeNames() {
		list = append(list, a.Shapes[n])
	}
	return list
}

var typesOnlyTplAPI = template.Must(template.New("api").Funcs(template.FuncMap{
	"ShapeListWithErrors": ShapeListWithErrors,
}).Parse(`
{{ $shapeList := ShapeListWithErrors $ }}
{{ range $_, $s := $shapeList }}
{{ if eq $s.Type "structure"}}{{ $s.GoCode }}{{ end }}

{{ end }}
`))

const copyrightHeaderTemplate = `// Copyright 2014-%d Amazon.com, Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//	http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.
`

var copyrightHeader = fmt.Sprintf(copyrightHeaderTemplate, time.Now().Year())

// return-based exit code so the 'defer' works
func _main() int {

	var typesOnly bool
	flag.BoolVar(&typesOnly, "typesOnly", false, "only generate types")
	flag.Parse()

	apiFile := "./api/api-2.json"
	var err error
	if typesOnly {
		err = genTypesOnlyAPI(apiFile)
	} else {
		err = genFull(apiFile)
	}
	if err != nil {
		fmt.Println(err)
		return 1
	}
	return 0
}

func genTypesOnlyAPI(file string) error {
	apiGen := &api.API{
		NoRemoveUnusedShapes:      true,
		NoRenameToplevelShapes:    true,
		NoGenStructFieldAccessors: true,
	}
	apiGen.Attach(file)
	apiGen.Setup()
	// to reset imports so that timestamp has an entry in the map.
	apiGen.APIGoCode()

	var buf bytes.Buffer
	err := typesOnlyTplAPI.Execute(&buf, apiGen)
	if err != nil {
		panic(err)
	}
	code := strings.TrimSpace(buf.String())
	code = util.GoFmt(code)

	// Ignore dir error, filepath will catch it for an invalid path.
	os.Mkdir(apiGen.PackageName(), 0755)
	// Fix imports.
	codeWithImports, err := imports.Process("", []byte(fmt.Sprintf("package %s\n\n%s", apiGen.PackageName(), code)), nil)
	if err != nil {
		fmt.Println(err)
		return err
	}
	outFile := filepath.Join(apiGen.PackageName(), "api.go")
	err = ioutil.WriteFile(outFile, []byte(fmt.Sprintf("%s\n%s", copyrightHeader, codeWithImports)), 0644)
	if err != nil {
		return err
	}
	return nil
}

func genFull(file string) error {
	// Heavily inspired by https://github.com/aws/aws-sdk-go/blob/45ba2f817fbf625fe48cf9b51fae13e5dfba6171/internal/model/cli/gen-api/main.go#L36
	// That code is copyright Amazon
	api := &api.API{}
	api.Attach(file)
	paginatorsFile := strings.Replace(file, "api-2.json", "paginators-1.json", -1)
	if _, err := os.Stat(paginatorsFile); err == nil {
		api.AttachPaginators(paginatorsFile)
	}

	docsFile := strings.Replace(file, "api-2.json", "docs-2.json", -1)
	if _, err := os.Stat(docsFile); err == nil {
		api.AttachDocs(docsFile)
	}
	api.Setup()

	processedApiCode, err := imports.Process("", []byte(fmt.Sprintf("package %s\n\n%s", api.PackageName(), api.APIGoCode())), nil)
	if err != nil {
		fmt.Println(err)
		return err
	}
	// Ignore dir error, filepath will catch it for an invalid path.
	os.Mkdir(api.PackageName(), 0755)
	outFile := filepath.Join(api.PackageName(), "api.go")
	err = ioutil.WriteFile(outFile, []byte(fmt.Sprintf("%s\n%s", copyrightHeader, processedApiCode)), 0644)
	if err != nil {
		return err
	}

	processedServiceCode, err := imports.Process("", []byte(fmt.Sprintf("package %s\n\n%s", api.PackageName(), api.ServiceGoCode())), nil)
	if err != nil {
		fmt.Println(err)
		return err
	}
	outFile = filepath.Join(api.PackageName(), "service.go")
	err = ioutil.WriteFile(outFile, []byte(fmt.Sprintf("%s\n%s", copyrightHeader, processedServiceCode)), 0644)
	if err != nil {
		return err
	}

	processedErrorCode, err := imports.Process("", []byte(fmt.Sprintf("package %s\n\n%s", api.PackageName(), api.APIErrorsGoCode())), nil)
	if err != nil {
		fmt.Println(err)
		return err
	}
	outFile = filepath.Join(api.PackageName(), "errors.go")
	err = ioutil.WriteFile(outFile, []byte(fmt.Sprintf("%s\n%s", copyrightHeader, processedErrorCode)), 0644)
	if err != nil {
		return err
	}

	return nil
}
