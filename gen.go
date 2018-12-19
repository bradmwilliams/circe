package main

import "bufio"
import "os"
import "log"
import "fmt"
import "strings"
import "path/filepath"
import "path"
import "reflect"

import "go/parser"
import "go/token"
import "go/ast"
import "go/types"


func check(e error) {
	if e != nil {
		panic(e)
	}
}

func outputStruct(scope *types.Scope, pkgBaseDir string, obj types.Object) {

	named := obj.Type().(*types.Named)  // .Type() returns the type of the language element. We assume it is a named type.
	underlyingStruct := named.Underlying().(*types.Struct) // The underlying type of the object should be a struct

	interfaceName := obj.Name() // obj.Name()  example: "NetworkConfig"
	javaFile, err := os.OpenFile(path.Join(pkgBaseDir, interfaceName + ".java"), os.O_CREATE | os.O_WRONLY, 0750)
	check(err)

	defer javaFile.Close()

	jw := bufio.NewWriter(javaFile)
	jw.WriteString(fmt.Sprintf("public interface %s {\n", interfaceName))

	for fi := 0; fi < underlyingStruct.NumFields(); fi++ {
		fieldVar := underlyingStruct.Field(fi)
		typeSplit := strings.Split(fieldVar.Type().String(), ".")
		typeName := typeSplit[len(typeSplit)-1]   // networkingconfig_types.NetworkConfig -> NetworkConfig ;  uint32 -> uint32

		fmt.Println(reflect.TypeOf(fieldVar.Type()))

		javaType := "None"
		if typeName == "uint32" || typeName == "*uint32" {
			javaType = "Long"
		} else if typeName == "string" || typeName == "*string" {
			javaType = "String"
		} else if typeName == "bool" || typeName == "*bool" {
			javaType = "Boolean"
		} else if typeName == "map[string][]string" {
			javaType = "Map<String,String[]>"
		} else if typeName == "map[string]string" {
			javaType = "Map<String,String>"
		} else {
			fmt.Println("About to lookup: " + typeName)
			nextObj := scope.Lookup(typeName)
			nextObjNamed := nextObj.Type().(*types.Named)

			switch t := nextObjNamed.Underlying().(type) {
			case *types.Struct:
			case *types.Basic:
				fmt.Printf("Basic! %s\n", t)
				continue
			default:
				fmt.Printf("Don't know how to output %s\n", t)
				continue
			}

			javaType = typeName
			fmt.Println("Next follows: " + typeName)
			fmt.Println(nextObj)
			outputStruct(scope, pkgBaseDir, nextObj)
		}

		var tag reflect.StructTag = reflect.StructTag(underlyingStruct.Tag(fi))
		jsonTag := tag.Get("json")
		jsonName := strings.Split(jsonTag, ",")[0]
		if len(jsonName) > 0 {
			if jsonName == "status" {
				continue
			}
			fmt.Println("Found jsonName: ", jsonName)

			jw.WriteString(fmt.Sprintf("\t//json:%s\n", jsonName))
			jw.WriteString(fmt.Sprintf("\tpublic %s get%s();\n", javaType, fieldVar.Name()))  // close 'public interface ... {'
		} else {
			panic(fmt.Sprintf("Unable to find json name for: %s", fieldVar.String()))
		}

		fmt.Println(fieldVar)
		fmt.Println()
	}


	jw.WriteString("}\n")  // close 'public interface ... {'
	jw.Flush()
}

func main() {

	genBaseDir := "gen/src/operator0/gen/"
	os.MkdirAll(genBaseDir, 0750)

	filename := "types/networkconfig_types.go"
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filename, nil, parser.ParseComments)

	if err != nil {
		log.Fatal(err)
		os.Exit(1)
	}

	pkgName := filepath.Base(filename)
	pkgName = strings.TrimSuffix(pkgName, ".go")
	pkgBaseDir := path.Join(genBaseDir, pkgName)
	os.MkdirAll(pkgBaseDir, 0750)

	var conf types.Config
	conf.Error = func(err error) {
		log.Println("Error during check: ", err)
	}

	pkg, err := conf.Check(pkgName, fset, []*ast.File{f}, nil)
	if err != nil {
		log.Println("Overall check error: ", err)
	}

	scope := pkg.Scope()
	obj := scope.Lookup("NetworkConfig")

	outputStruct(scope, pkgBaseDir, obj)
	return
}
