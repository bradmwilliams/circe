package main

import "bufio"
import "os"
import "log"
import "fmt"
import "strings"
import "path/filepath"
import "path"
import "reflect"
import "io/ioutil"

import "go/parser"
import "go/token"
import "go/ast"
import "go/types"
import "unicode"

import "gopkg.in/yaml.v2"

var outputDone = make(map[string]bool)
var pkgDir string

func check(e error) {
	if e != nil {
		panic(e)
	}
}

type myimporter struct {}

func (importer myimporter) Import(path string) (*types.Package, error) {
	fmt.Println("Received request for import of: " + path)
	return nil, nil
}

func toLowerCamelcase(name string) string {
	runes := []rune(name)
	newRunes := make([]rune, len(runes))
	toggle := true

	// TLS ->  tls
	// SomeValue -> someValue
	// someValue -> someValue

	for i, r := range runes {
		if unicode.IsLower(r) {
			toggle = false
		}
		if toggle && unicode.IsUpper(r) {
			// If "TLSVerify", we want tlsVerify, so look ahead unless we are at the end
			if i == 0 || len(runes) == i+1 || unicode.IsLower(runes[i+1]) == false {
				newRunes[i] = unicode.ToLower(r)
			} else {
				newRunes[i] = r
			}
		} else {
			newRunes[i] = r
		}
	}

	return string(newRunes)
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

		if len(jsonName) == 0 {
			jsonName = toLowerCamelcase(fieldVar.Name())
		}

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

type OperatorConfig struct {
	File string `yaml:"file"`
	GoType string `yaml:"go_type"`
	KubeName string `yaml:"kube_name"`
	Namespace string `yaml:"namespace"`
}

type GuideYaml struct {
	ClusterConfig []OperatorConfig `yaml:"ClusterDefinition"`
}

func main() {

	yamlFile, err := ioutil.ReadFile("guide.yaml")
	check(err)
	guide := GuideYaml{}
	yaml.Unmarshal(yamlFile, &guide)
	fmt.Println(fmt.Sprintf("Found %d ClusterDefinition rules", len(guide.ClusterConfig)))

	outputDir := "../circe-java-gen/src/main/java"
	basePkg := "com.redhat.openshift.circe.gen"
	basePackageDir := path.Join(outputDir, strings.Replace(basePkg, ".", "/", -1))

	astFiles := make([]*ast.File, 0)

	fset := token.NewFileSet()
	for _, oc := range guide.ClusterConfig {
		f, err := parser.ParseFile(fset, oc.File, nil, parser.ParseComments)
		check(err)
		astFiles = append(astFiles, f)
	}

	importer := myimporter{}
	conf := types.Config{Importer: importer}
	conf.Error = func(err error) {
		log.Println("Error during check: ", err)
	}

	pkg, err := conf.Check("parse", fset, astFiles, nil)
	if err != nil {
		log.Println("Overall check error: ", err)
	}

	packageNames := make([]string, 0)

	for _, oc := range guide.ClusterConfig {
		filename := oc.File

		shortPkgName := filepath.Base(filename)
		shortPkgName = strings.TrimSuffix(shortPkgName, ".go")
		packageName := fmt.Sprintf("%s.%s", basePkg, shortPkgName)
		packageNames = append(packageNames, packageName)

		pkgDir = path.Join(basePackageDir, shortPkgName)
		os.MkdirAll(pkgDir, 0750)


		scope := pkg.Scope()
		obj := scope.Lookup(oc.GoType)
		fmt.Println("Loaded", oc.GoType, "=>", obj.String())
		named := obj.Type().(*types.Named)  // .Type() returns the type of the language element. We assume it is a named type.
		underlyingStruct := named.Underlying().(*types.Struct) // The underlying type of the object should be a struct
		structName := obj.Name() // obj.Name()  example: "NetworkConfig"

		sg := StructGen{
			pkgDir:pkgDir,
			pkg:packageName,
		}

		sg.outputStruct(structName, underlyingStruct)
	}

	javaFile, err := os.OpenFile(path.Join(basePackageDir, "ClusterDefinition.java"), os.O_CREATE | os.O_TRUNC | os.O_WRONLY, 0750)
	check(err)

	defer javaFile.Close()

	jw := bufio.NewWriter(javaFile)
	jw.WriteString("package " + basePkg + ";\n\n")

	jw.WriteString("import java.util.*;\n")

	for _, packageName := range packageNames {
		jw.WriteString("import " + packageName + ".*;\n")
	}

	jw.WriteString("\npublic interface ClusterDefinition {\n\n")
	for _, oc := range guide.ClusterConfig {
		jw.WriteString("\t" + oc.GoType + " get" + oc.GoType + "();\n\n")
	}
	jw.WriteString("\n}\n")
	jw.Flush()

	return
}



