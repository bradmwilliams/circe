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

var outputDone = make(map[string]bool)
var pkgDir string

func check(e error) {
	if e != nil {
		panic(e)
	}
}

type StructGen struct {
	pkgDir string
	pkg    string
}

func (sg *StructGen) getJavaType(typ types.Type) string {
	typeSplit := strings.Split(typ.String(), ".")
	typeName := typeSplit[len(typeSplit) - 1]   // networkingconfig_types.NetworkConfig -> NetworkConfig ;  uint32 -> uint32
	typeName = strings.Trim(typeName, "*") // ignore pointer vs non-pointer

	switch ct := typ.(type) {
	case *types.Basic:
		if strings.HasPrefix(typeName, "uint") || strings.HasPrefix(typeName, "int") || strings.HasPrefix(typeName, "rune") || strings.HasPrefix(typeName, "byte"){
			// We're not trying to match exact int size. That is left of to the user.
			return "Long"
		} else if typeName == "string" {
			return "String"
		} else if typeName == "bool" {
			return "Boolean"
		} else if strings.HasPrefix(typeName, "float") {
			return "Double"
		} else {
			panic("I don't know how to translate type: " + typeName)
		}

	case *types.Slice:
		return "List<" + sg.getJavaType(ct.Elem()) + ">"
	case *types.Array:
		return "List<" + sg.getJavaType(ct.Elem()) + ">"
	case *types.Map:
		return "Map<" + sg.getJavaType(ct.Key()) + "," + sg.getJavaType(ct.Elem()) + ">"
	case *types.Pointer:
		return sg.getJavaType(ct.Elem())
	case *types.Named:
		// e.g. 'type SomeNamedType struct'    OR    'type SomeNamedType string|uint32...' <==basic
		switch ut := ct.Underlying().(type) {
		case *types.Struct:
			sg.outputStruct(typeName, ut)
			return typeName
		default:
			return sg.getJavaType(ut)
		}
	default:
		panic(fmt.Sprintf("I don't know how to translate type: %s", reflect.TypeOf(typ)))
	}
}

func (sg *StructGen) outputStruct(structName string, underlyingStruct *types.Struct) {

	_, ok := outputDone[structName]
	if ok {
		// if the struct has already been output
		return
	} else {
		outputDone[structName] = true
	}

	javaFile, err := os.OpenFile(path.Join(sg.pkgDir, structName + ".java"), os.O_CREATE | os.O_TRUNC | os.O_WRONLY, 0750)
	check(err)

	defer javaFile.Close()

	jw := bufio.NewWriter(javaFile)

	jw.WriteString(fmt.Sprintf("package %s;\n\n", sg.pkg))

	jw.WriteString("import java.util.*;\n\n")

	jw.WriteString(fmt.Sprintf("public interface %s {\n", structName))

	for fi := 0; fi < underlyingStruct.NumFields(); fi++ {
		fieldVar := underlyingStruct.Field(fi)
		fmt.Println(fmt.Sprintf("Processing field: %s", fieldVar))
		javaType := sg.getJavaType(fieldVar.Type())

		var tag reflect.StructTag = reflect.StructTag(underlyingStruct.Tag(fi))
		jsonTag := tag.Get("json")
		jsonName := strings.Split(jsonTag, ",")[0]
		if len(jsonName) > 0 {
			if jsonName == "status" {
				continue
			}
			fmt.Println("Found jsonName: ", jsonName)

			jw.WriteString(fmt.Sprintf("\t//json:%s\n", jsonName))
			jw.WriteString(fmt.Sprintf("\t%s get%s();\n", javaType, fieldVar.Name()))  // close 'public interface ... {'
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

	outputDir := "../operator0-java-gen/src/main/java"
	basePkg := "operator0.gen"
	basePackageDir := path.Join(outputDir, strings.Replace(basePkg, ".", "/", -1))

	filename := "types/networkconfig_types.go"
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filename, nil, parser.ParseComments)

	if err != nil {
		log.Fatal(err)
		os.Exit(1)
	}

	shortPkgName := filepath.Base(filename)
	shortPkgName = strings.TrimSuffix(shortPkgName, ".go")
	packageName := fmt.Sprintf("%s.%s", basePkg, shortPkgName)

	pkgDir = path.Join(basePackageDir, shortPkgName)
	os.MkdirAll(pkgDir, 0750)

	var conf types.Config
	conf.Error = func(err error) {
		log.Println("Error during check: ", err)
	}

	pkg, err := conf.Check(shortPkgName, fset, []*ast.File{f}, nil)
	if err != nil {
		log.Println("Overall check error: ", err)
	}

	scope := pkg.Scope()
	obj := scope.Lookup("NetworkConfig")
	named := obj.Type().(*types.Named)  // .Type() returns the type of the language element. We assume it is a named type.
	underlyingStruct := named.Underlying().(*types.Struct) // The underlying type of the object should be a struct
	structName := obj.Name() // obj.Name()  example: "NetworkConfig"

	sg := StructGen{
		pkgDir:pkgDir,
		pkg:packageName,
	}
	sg.outputStruct(structName, underlyingStruct)


	//outputStruct(scope, pkgBaseDir, obj)
	return
}
