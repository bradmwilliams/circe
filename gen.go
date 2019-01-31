package main

import "bufio"
import "os"
import "log"
import "fmt"
import "strings"
import "path"
import "reflect"
import "io/ioutil"
import "io"
import "errors"

import "go/parser"
import "go/token"
import "go/ast"
import "go/types"
import "unicode"

import (
	"gopkg.in/yaml.v2"
)

var outputDone = make(map[string]bool)

func check(e error) {
	if e != nil {
		panic(e)
	}
}

type dynimporter struct {}

var imported map[string]*types.Package = make(map[string]*types.Package)

func (importer dynimporter) Import(path string) (*types.Package, error) {

	if v, ok := imported[path]; ok {
		return v, nil
	}

	fmt.Println("\n\nProcessing import", path)
	if (!strings.HasPrefix(path, "github.com/") && !strings.Contains(path, "k8s")) {
		fmt.Println("SKIPPING!", path)
		return nil, errors.New("Skipping since it is outside k8s/openshift")
	}

	fsetBase := token.NewFileSet()
	asts, err := parser.ParseDir(fsetBase, "/home/jupierce/go/src/" + path, nil, parser.ParseComments)

	conf := types.Config{IgnoreFuncBodies: true, Importer:importer, DisableUnusedImportCheck: true}
	conf.Error = func(err error) {
		log.Println("Error during check 2: ", err)
	}

	for name, pkgAst := range asts {
		fmt.Printf("Processing pkgAst: %s", name)

		// A package may contain, e.g. v1 and v1_test. Ignore the test package.
		if strings.Contains(name, "_test") {
			continue
		}

		var fs []*ast.File

		for _, f := range pkgAst.Files {
			fs = append(fs, f)
		}

		pkg, err := conf.Check(path, fsetBase, fs, nil)
		if err != nil {
			log.Println("Overall check error 2: ", err)
		}
		imported[path] = pkg
		return pkg, nil
	}

	return nil, err
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

func copy(src, dst string) (int64, error) {
        sourceFileStat, err := os.Stat(src)
        if err != nil {
                return 0, err
        }

        if !sourceFileStat.Mode().IsRegular() {
                return 0, fmt.Errorf("%s is not a regular file", src)
        }

        source, err := os.Open(src)
        if err != nil {
                return 0, err
        }
        defer source.Close()

        destination, err := os.Create(dst)
        if err != nil {
                return 0, err
        }
        defer destination.Close()
        nBytes, err := io.Copy(destination, source)
        return nBytes, err
}

type StructGen struct {
	goPkgDir      string
	javaPkgDir    string
	pkg           string
	implPkg           string
	config        OperatorConfig
}

func (sg *StructGen) getJavaType(typ types.Type) string {
	typeSplit := strings.Split(typ.String(), ".")
	typeName := typeSplit[len(typeSplit) - 1]   // networkingconfig_types.NetworkConfig -> NetworkConfig ;  uint32 -> uint32
	typeName = strings.Trim(typeName, "*") // ignore pointer vs non-pointer

	fmt.Println("Attempt to coerce type to java: " + typeName)

	if typeName == "RawExtension" {
		return "String"
	}

	if typeName == "Quantity" {
		return "Quantity"
	}

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
			panic(fmt.Sprintf("I don't know how to translate: %s %T", ct.String(), typ))
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

	javaFile, err := os.OpenFile(path.Join(sg.javaPkgDir, structName + ".java"), os.O_CREATE | os.O_TRUNC | os.O_WRONLY, 0750)
	check(err)

	defer javaFile.Close()

	jw := bufio.NewWriter(javaFile)

	jw.WriteString(fmt.Sprintf("package %s;\n\n", sg.pkg))

	jw.WriteString("import " + sg.implPkg + ".*;\n")
	jw.WriteString("import java.util.*;\n\n")

	jw.WriteString(fmt.Sprintf("public interface %s {\n", structName))

	for fi := 0; fi < underlyingStruct.NumFields(); fi++ {
		fieldVar := underlyingStruct.Field(fi)
		fmt.Println(fmt.Sprintf("Processing field: %s", fieldVar))

		fmt.Printf("Testing: %s\n", fieldVar.Type().String())

		if strings.HasSuffix(fieldVar.Type().String(), "TypeMeta") {
			fmt.Println("Skipping TypeMeta")
			jw.WriteString(fmt.Sprintf("\tdefault String getKind() { return %q; }\n", structName))
			goPkgSplit := strings.Split(strings.TrimRight(sg.goPkgDir, "/"), "/")
			apiVersion := sg.config.KubeVersion
			if len(apiVersion) == 0 {
				apiVersion = goPkgSplit[len(goPkgSplit)-1]
				if strings.HasPrefix(apiVersion, "v") == false {
					panic("Unable to autodetect apiVersion for package (add kube_version in guide.yaml): " + sg.config.PkgDir)
				}
			}

			if len(sg.config.KubeGroup) > 0 {
				apiVersion = sg.config.KubeGroup + "/" + apiVersion
			}
			jw.WriteString(fmt.Sprintf("\tdefault String getApiVersion() { return %q; }\n", apiVersion))
			continue
		}


		if strings.HasSuffix(fieldVar.Type().String(), "ObjectMeta") {
			fmt.Println("Skipping ObjectMeta")
			jw.WriteString(fmt.Sprintf("\tdefault ObjectMeta getMetadata() { return new ObjectMeta(%q, %q); }\n", sg.config.KubeNamespace, sg.config.KubeName))
			continue
		}

		if strings.HasSuffix(fieldVar.Type().String(), "runtime.Object") {
			if strings.HasPrefix(fieldVar.Type().String(), "[]") {
				jw.WriteString(fmt.Sprintf("\t%s get%s();\n", "List<YamlProvider>", fieldVar.Name()))
			} else {
				jw.WriteString(fmt.Sprintf("\t%s get%s();\n", "YamlProvider", fieldVar.Name()))
			}
			continue
		}


		var tag reflect.StructTag = reflect.StructTag(underlyingStruct.Tag(fi))
		jsonTag := tag.Get("json")
		jsonName := strings.Split(jsonTag, ",")[0]

		if len(jsonName) == 0 {
			jsonName = toLowerCamelcase(fieldVar.Name())
		}

		if len(jsonName) > 0 {
			fmt.Println("Found jsonName: ", jsonName)
			if jsonName == "status" {
				fmt.Println("Skipping status field")
				continue
			}

			javaType := sg.getJavaType(fieldVar.Type())
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
	PkgDir        string `yaml:"package"`
	GoType        string `yaml:"go_type"`
	KubeGroup      string `yaml:"kube_group"`
	KubeVersion      string `yaml:"kube_version"`
	KubeName      string `yaml:"kube_name"`
	KubeNamespace string `yaml:"kube_namespace"`
	PackageOnly bool `yaml:"package_only"`
}

type Unit struct {
	Elements []OperatorConfig `yaml:"elements"`
}


type GuideYaml struct {
	Units map[string]Unit `yaml:"units"`
}

func main() {

	/*
	d := dynimporter{}
	pkg, err := d.Import("sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1")
	fmt.Printf("%v\n", pkg.Scope().Lookup("MachineSet"))
	os.Exit(1)
	*/

	yamlFile, err := ioutil.ReadFile("guide.yaml")
	check(err)
	guide := GuideYaml{}
	yaml.Unmarshal(yamlFile, &guide)
	fmt.Println(fmt.Sprintf("Found %d ClusterDefinition rules", len(guide.Units)))

	outputDir := "../circe-java-gen/src/main/java"
	basePkg := "com.redhat.openshift.circe.gen"
	basePackageDir := path.Join(outputDir, strings.Replace(basePkg, ".", "/", -1))
	implPkg := "com.redhat.openshift.circe.gen.impl"
	implPackageDir := path.Join(outputDir, strings.Replace(implPkg, ".", "/", -1))

	d := dynimporter{}
	packageNames := make([]string, 0)

	for name, unit := range guide.Units {
		fmt.Println("Generating unit: " + name)
		for _, oc := range unit.Elements {
			goPkgDir := oc.PkgDir
			pkg, err := d.Import(goPkgDir)
			check(err)

			shortPkgName := strings.ToLower(oc.GoType)
			packageName := fmt.Sprintf("%s.%s", basePkg, shortPkgName)
			packageNames = append(packageNames, packageName)

			javaPkgDir := path.Join(basePackageDir, shortPkgName)
			os.MkdirAll(javaPkgDir, 0750)

			scope := pkg.Scope()
			obj := scope.Lookup(oc.GoType)
			fmt.Println("Loaded", oc.GoType, "=>", obj.String())
			named := obj.Type().(*types.Named)  // .Type() returns the type of the language element. We assume it is a named type.
			underlyingStruct := named.Underlying().(*types.Struct) // The underlying type of the object should be a struct
			structName := obj.Name() // obj.Name()  example: "NetworkConfig"

			sg := StructGen{
				goPkgDir: goPkgDir,
				javaPkgDir: javaPkgDir,
				pkg: packageName,
				implPkg: implPkg,
				config: oc,
			}

			sg.outputStruct(structName, underlyingStruct)

		}

		javaFile, err := os.OpenFile(path.Join(basePackageDir, name + ".java"), os.O_CREATE | os.O_TRUNC | os.O_WRONLY, 0750)
		check(err)

		jw := bufio.NewWriter(javaFile)
		jw.WriteString("package " + basePkg + ";\n\n")

		jw.WriteString("import java.util.*;\n")

		for _, packageName := range packageNames {
			jw.WriteString("import " + packageName + ".*;\n")
		}

		jw.WriteString("import " + implPkg + ".*;\n")

		jw.WriteString("\npublic interface " + name + " {\n\n")
		for _, oc := range unit.Elements {
			if oc.PackageOnly == false {
				jw.WriteString("\t" + oc.GoType + " get" + oc.GoType + "();\n\n")
			}
		}
		jw.WriteString("\n}\n")
		jw.Flush()
		javaFile.Close()
	}

	// Output ObjectMeta helper and other impl classes
	os.MkdirAll(implPackageDir, 0750)
	dir, err := os.Getwd()
	check(err)

	_, err = copy( path.Join(dir, "src/render/java", strings.Replace(implPkg, ".", "/", -1), "ObjectMeta.java"), path.Join(implPackageDir, "ObjectMeta.java"))
	check(err)
	_, err = copy( path.Join(dir, "src/render/java", strings.Replace(implPkg, ".", "/", -1), "BaseObject.java"), path.Join(implPackageDir, "BaseObject.java"))
	check(err)

	return
}



