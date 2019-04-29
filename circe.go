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

func check(e error) {
	if e != nil {
		panic(e)
	}
}

type dynimporter struct {
	// paths which will be scanned for /<go package> during Import
	paths     []string
	vendorDir string
}

var imported map[string]*types.Package = make(map[string]*types.Package)

func (importer dynimporter) Import(goPkgName string) (*types.Package, error) {

	if v, ok := imported[goPkgName]; ok {
		return v, nil
	}

	fmt.Println("\n\nImport requested", goPkgName)
	if !strings.HasPrefix(goPkgName, "github.com/") && !strings.Contains(goPkgName, "k8s") {
		fmt.Println("SKIPPING!", goPkgName)
		return nil, errors.New("Skipping since it is outside k8s/openshift")
	}
	fmt.Println("\n\nProcessing import", goPkgName)

	fsetBase := token.NewFileSet()

	for _, srcDir := range importer.paths {
		checkDir := path.Join(srcDir, goPkgName)
		_, err := os.Stat(checkDir)

		if err != nil {
			if os.IsNotExist(err) {
				continue
			} else {
				return nil, errors.New("Error scanning for package: " + err.Error())
			}
		}

		nextImporter := importer

		// Once a src directory is found in a particular src directory, prepend the vendor source
		// directory if one has been specified.
		if len(importer.vendorDir) > 0 {
			vendorPath := path.Join(srcDir, importer.vendorDir)
			paths := append([]string{vendorPath}, importer.paths...)
			nextImporter = dynimporter{
				paths:     paths,
				vendorDir: "",
			}
		}

		asts, err := parser.ParseDir(fsetBase, checkDir, nil, parser.ParseComments)

		conf := types.Config{IgnoreFuncBodies: true, Importer: nextImporter, DisableUnusedImportCheck: true}
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

			pkg, err := conf.Check(goPkgName, fsetBase, fs, nil)
			if err != nil {
				log.Println("Overall check error 2: ", err)
			}
			imported[goPkgName] = pkg
			return pkg, nil
		}

	}

	return nil, errors.New("Unable to find source package: " + goPkgName)
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
	goPkgDir   string
	javaPkgDir string
	pkg        string
	beanPkg    string
	config     ModelConfig
	outputDone map[string]bool
}

// If there is a direct mapping or helper class for a type in Java land, add it to this
// map so that no effort will be made trying to map the structure into Java.
var simpleJavaTypeMap = map[string]string{
	"RawExtension": "Bean",
	"Quantity":     "Quantity",
	"Secret":       "Secret",
	"Interface":    "Bean",
	"Duration":     "Duration",
}

func out(depth int, msg string) {
	for i := 0; i < depth; i++ {
		fmt.Print("\t")
	}
	fmt.Println(msg)
}

func (sg *StructGen) getJavaType(depth int, typ types.Type) string {
	typeSplit := strings.Split(typ.String(), ".")
	typeName := typeSplit[len(typeSplit)-1] // networkingconfig_types.NetworkConfig -> NetworkConfig ;  uint32 -> uint32
	typeName = strings.Trim(typeName, "*")  // ignore pointer vs non-pointer

	out(depth, "Attempt to coerce type to java: "+typeName)

	if simpleType, ok := simpleJavaTypeMap[typeName]; ok {
		out(depth, "  Coerced to simple type: "+simpleType)
		return simpleType
	}

	switch ct := typ.(type) {
	case *types.Basic:
		if strings.HasPrefix(typeName, "uint") || strings.HasPrefix(typeName, "int") || strings.HasPrefix(typeName, "rune") || strings.HasPrefix(typeName, "byte") {
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
		return "List<" + sg.getJavaType(depth, ct.Elem()) + ">"
	case *types.Array:
		return "List<" + sg.getJavaType(depth, ct.Elem()) + ">"
	case *types.Map:
		return "Map<" + sg.getJavaType(depth, ct.Key()) + "," + sg.getJavaType(depth, ct.Elem()) + ">"
	case *types.Pointer:
		return sg.getJavaType(depth, ct.Elem())
	case *types.Named:
		// e.g. 'type SomeNamedType struct'    OR    'type SomeNamedType string|uint32...' <==basic
		switch ut := ct.Underlying().(type) {
		case *types.Struct:
			out(depth, "  Coercing to complex struct...")
			sg.outputStruct(depth+1, typeName, ut, "")
			return typeName
		default:
			out(depth, "  Coercing to java type")
			return sg.getJavaType(depth, ut)
		}
	default:
		panic(fmt.Sprintf("I don't know how to translate type: %s", reflect.TypeOf(typ)))
	}
}

func underscoreVersion(version string) string {
	return strings.Replace(version, ".", "_", -1)
}

func (sg *StructGen) outputStruct(depth int, structName string, underlyingStruct *types.Struct, classNameOverride string) string {

	goPkgSplit := strings.Split(strings.TrimRight(sg.goPkgDir, "/"), "/")
	apiVersion := sg.config.KubeVersion
	if len(apiVersion) == 0 {
		apiVersion = goPkgSplit[len(goPkgSplit)-1]
		if strings.HasPrefix(apiVersion, "v") == false {
			panic("Unable to autodetect apiVersion for package (add kube_version in guide.yaml): " + sg.config.PkgDir)
		}
	}

	version_underscore := underscoreVersion(apiVersion)

	modulePackage := fmt.Sprintf("%s.%s", sg.pkg, version_underscore)
	modulePkgDir := path.Join(sg.javaPkgDir, version_underscore)

	structId := modulePackage + "." + structName

	_, ok := sg.outputDone[structId]
	if ok {
		// if the struct has already been output for this package
		return modulePackage
	} else {
		sg.outputDone[structId] = true
	}

	if len(classNameOverride) == 0 {
		classNameOverride = structName
	}

	os.MkdirAll(modulePkgDir, 0755)
	javaFilename := path.Join(modulePkgDir, classNameOverride+".java")
	out(depth, "   Opening file: "+javaFilename)

	javaFile, err := os.OpenFile(javaFilename, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0750)
	check(err)

	defer javaFile.Close()

	jw := bufio.NewWriter(javaFile)

	jw.WriteString("// GENERATED FILE -- DO NOT ALTER (circe.go)\n\n")
	jw.WriteString(fmt.Sprintf("package %s;\n", modulePackage))

	jw.WriteString("import " + sg.beanPkg + ".*;\n")
	jw.WriteString("import com.github.openshift.circe.yaml.*;\n")
	jw.WriteString("import java.util.*;\n\n")

	jw.WriteString(fmt.Sprintf("public interface %s extends Bean {\n\n", classNameOverride))

	ezDefaults := make([]string, 0)

	writeGetterMethodSig := func(methodType string, fieldName string, inline bool, jsonName string) {
		jw.WriteString(fmt.Sprintf("\t@YamlPropertyName(value=%q)\n", jsonName))
		ezDefaults = append(ezDefaults, fmt.Sprintf("\t\t@YamlPropertyName(value=%q)\n", jsonName))
		if inline {
			jw.WriteString("\t@YamlPropertyInline\n")
			ezDefaults = append(ezDefaults, "\t\t@YamlPropertyInline\n")
		}
		jw.WriteString(fmt.Sprintf("\t%s get%s() throws Exception;\n\n", methodType, fieldName))
		ezDefaults = append(ezDefaults, fmt.Sprintf("\t\tdefault %s get%s() throws Exception { return null; }\n\n", methodType, fieldName))
	}

	for fi := 0; fi < underlyingStruct.NumFields(); fi++ {
		fieldVar := underlyingStruct.Field(fi)

		out(depth, fmt.Sprintf("Processing field: %s", fieldVar))

		out(depth, "Testing: "+fieldVar.Type().String())

		if strings.HasSuffix(fieldVar.Type().String(), "TypeMeta") {
			out(depth, "Skipping TypeMeta")
			jw.WriteString(fmt.Sprintf("\tdefault String getKind() { return %q; }\n", structName))

			if len(sg.config.KubeGroup) > 0 {
				apiVersion = sg.config.KubeGroup + "/" + apiVersion
			}
			jw.WriteString(fmt.Sprintf("\tdefault String getApiVersion() { return %q; }\n", apiVersion))
			continue
		}

		if strings.HasSuffix(fieldVar.Type().String(), "ObjectMeta") {
			out(depth, "Processing ObjectMeta")

			jw.WriteString("\t@YamlPropertyIgnore\n")
			jw.WriteString(fmt.Sprintf("\tdefault String _getGeneratorNamespaceHint() { return %q; }\n", sg.config.KubeNamespace))

			jw.WriteString("\t@YamlPropertyIgnore\n")
			jw.WriteString(fmt.Sprintf("\tdefault String _getGeneratorNameHint() { return %q; }\n", sg.config.KubeName))

			if sg.config.List || sg.config.Map {
				jw.WriteString("\tObjectMeta getMetadata() throws Exception;\n")
			} else {
				jw.WriteString("\tdefault ObjectMeta getMetadata() throws Exception { return new ObjectMeta(_getGeneratorNamespaceHint(), _getGeneratorNameHint()); }\n")
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

			if strings.HasSuffix(fieldVar.Type().String(), "runtime.Object") {
				if strings.HasPrefix(fieldVar.Type().String(), "[]") {
					writeGetterMethodSig("List<Bean>", fieldVar.Name(), false, jsonName)
				} else {
					writeGetterMethodSig("Bean", fieldVar.Name(), false, jsonName)
				}
				continue
			}

			out(depth, "Found jsonName: "+jsonName)
			if jsonName == "status" {
				out(depth, "Skipping status field")
				continue
			}

			if jsonName == "-" {
				// https://github.com/openshift/origin/blob/29f688498ec658a22daac7dbe37502d05b7af64d/pkg/image/apiserver/admission/apis/imagepolicy/v1/types.go#L119
				out(depth, "Skipping internal field")
				continue
			}

			javaType := sg.getJavaType(depth, fieldVar.Type())
			inline := fieldVar.Anonymous() || strings.Contains(jsonTag, ",inline")
			writeGetterMethodSig(javaType, fieldVar.Name(), inline, jsonName)
		} else {
			panic(fmt.Sprintf("Unable to find json name for: %s", fieldVar.String()))
		}

		out(depth, fieldVar.String())
		out(depth, "")
	}

	jw.WriteString(fmt.Sprintf("\tinterface EZ extends %s {\n\n", classNameOverride))
	for _, ezDefault := range ezDefaults {
		jw.WriteString(ezDefault)
	}
	jw.WriteString("\t}\n\n") // close EZ interface

	jw.WriteString("}\n") // close 'public interface ... {'
	jw.Flush()

	return modulePackage
}

type ModelConfig struct {
	// Override the name of the Java class name
	Class string `yaml:"class"`

	// The go package
	PkgDir string `yaml:"package"`

	// Vendor directory with which to satisfy dependencies
	VendorDir string `yaml:"vendor"`

	// The name of the go type to model
	GoType string `yaml:"go_type"`
	// Change the name of the generate java method
	InterfaceMethodName string `yaml:"interface_method_name"`
	KubeGroup           string `yaml:"kube_group"`
	KubeVersion         string `yaml:"kube_version"`
	KubeName            string `yaml:"kube_name"`
	KubeNamespace       string `yaml:"kube_namespace"`
	ModelOnly           bool   `yaml:"model_only"`
	List                bool   `yaml:"list"`
	Map                 bool   `yaml:"map"`

	// Elements that should only be modeled
	SubModels []ModelConfig `yaml:"sub_models"`
}

type Unit struct {
	// The name of the Java interface that must be implemented for the Unit
	Class string `yaml:"class"`

	// Human name for the Unit
	Name string `yaml:"name"`

	// Which version of OpenShift the Unit is appropriate for
	Version string `yaml:"version"`

	// The ModelConfig elements that make up the Unit
	Elements []ModelConfig `yaml:"elements"`

	JavaImports []string `yaml:"imports"`
}

type GuideYaml struct {
	Units []Unit `yaml:"units"`
}

func main() {

	yamlFile, err := ioutil.ReadFile("guide.yaml")
	check(err)
	guide := GuideYaml{}
	yaml.Unmarshal(yamlFile, &guide)
	fmt.Println(fmt.Sprintf("Found %d units", len(guide.Units)))

	outputDir := "render/src/generated/java"
	os.RemoveAll(outputDir)

	basePkg := "com.github.openshift.circe.gen"
	basePackageDir := path.Join(outputDir, strings.Replace(basePkg, ".", "/", -1))
	beanPkg := "com.github.openshift.circe.beans"

	gopath := os.Getenv("GOPATH")
	if len(gopath) == 0 {
		fmt.Println("Please set GOPATH environment variable before running (colon delimited list is supported)")
		os.Exit(1)
	}

	// Each entry in the gopath should have a 'src' directory within it. Populate a list of directories the
	// importer should search.
	importerPaths := make([]string, 0)
	for _, entry := range strings.Split(gopath, ":") {
		importerPaths = append(importerPaths, path.Join(entry, "src"))
	}
	fmt.Println(fmt.Sprintf("Loaded go paths: %v", importerPaths))

	os.MkdirAll(basePackageDir, 0750)
	unitsEnumType, err := os.OpenFile(path.Join(basePackageDir, "UnitType.java"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0750)
	check(err)

	unitEnumWriter := bufio.NewWriter(unitsEnumType)
	unitEnumWriter.WriteString("// GENERATED FILE -- DO NOT ALTER (circe.go)\n\n")

	unitEnumWriter.WriteString("package " + basePkg + ";\n\n")

	unitEnumWriter.WriteString("\npublic enum UnitType {\n\n")

	for _, unit := range guide.Units {
		className := unit.Class

		// Within a unit, definitions can be ordered by the renderer to ensure they are populated
		// on the cluster in specific order. Their order in the source yaml is honored.
		renderOrderHint := 1

		renderModel := func(modelConfig ModelConfig) {

			// If the go type has a Java type already associated, don't bother generating java
			if _, ok := simpleJavaTypeMap[modelConfig.GoType]; ok {
				return
			}

			// Create an importer that will search go elements for the main package
			// Also specify vendor directory which element may have specified.
			importer := dynimporter{
				paths:     importerPaths,
				vendorDir: modelConfig.VendorDir,
			}

			goPkgDir := modelConfig.PkgDir
			pkg, err := importer.Import(goPkgDir)
			check(err)

			className := modelConfig.GoType
			if len(modelConfig.Class) > 0 {
				className = modelConfig.Class
			}

			shortPkgName := strings.ToLower(className)
			packageName := fmt.Sprintf("%s.%s", basePkg, shortPkgName)

			javaPkgDir := path.Join(basePackageDir, shortPkgName)
			os.MkdirAll(javaPkgDir, 0750)

			scope := pkg.Scope()
			obj := scope.Lookup(modelConfig.GoType)
			fmt.Println("Loaded", modelConfig.GoType, "=>", obj.String())
			named := obj.Type().(*types.Named)                     // .Type() returns the type of the language element. We assume it is a named type.
			underlyingStruct := named.Underlying().(*types.Struct) // The underlying type of the object should be a struct
			structName := obj.Name()                               // obj.Name()  example: "NetworkConfig"

			sg := StructGen{
				goPkgDir:   goPkgDir,
				javaPkgDir: javaPkgDir,
				pkg:        packageName,
				beanPkg:    beanPkg,
				config:     modelConfig,
				outputDone: make(map[string]bool),
			}

			out(0, "Processing unit: "+unit.Name+"=>"+structName)
			modulePackage := sg.outputStruct(1, structName, underlyingStruct, modelConfig.Class)
			unit.JavaImports = append(unit.JavaImports, modulePackage)

		}

		fmt.Println("Generating unit: " + className)
		for _, modelConfig := range unit.Elements {

			renderModel(modelConfig)

			for _, sub := range modelConfig.SubModels {
				sub.ModelOnly = true
				renderModel(sub)
			}

		}

		unitUnderscoreVersion := underscoreVersion(unit.Version)

		unitPkgDir := path.Join(basePackageDir, "units", unitUnderscoreVersion)
		unitPkg := strings.Join([]string{basePkg, "units", unitUnderscoreVersion}, ".")
		unitClassName := strings.Join([]string{basePkg, "units", unitUnderscoreVersion, className, "class"}, ".")
		unitHumanName := fmt.Sprintf("%s-%s", unit.Version, unit.Name)

		os.MkdirAll(unitPkgDir, 0755)
		javaFile, err := os.OpenFile(path.Join(unitPkgDir, className+".java"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0750)
		check(err)

		unitEnumWriter.WriteString(fmt.Sprintf("\t%s_%s(%s, %q, %q),\n", unitUnderscoreVersion, unit.Name, unitClassName, unit.Name, unitHumanName))

		jw := bufio.NewWriter(javaFile)
		jw.WriteString("// GENERATED FILE -- DO NOT ALTER (circe.go)\n\n")
		jw.WriteString("package " + unitPkg + ";\n\n")

		jw.WriteString("import java.util.*;\n")
		jw.WriteString("import com.github.openshift.circe.yaml.*;\n")

		for _, packageName := range unit.JavaImports {
			jw.WriteString("import " + packageName + ".*;\n")
		}

		jw.WriteString("import " + beanPkg + ".*;\n")

		ezDefaults := make([]string, 0)

		writeMethodSig := func(methodType string, methodName string) {
			ezDefaults = append(ezDefaults, fmt.Sprintf("\t\t@RenderOrder(value =\"%04d\")\n", renderOrderHint))
			ezDefaults = append(ezDefaults, fmt.Sprintf("\t\tdefault %s %s() throws Exception { return null; }\n\n", methodType, methodName))
			jw.WriteString(fmt.Sprintf("\t@RenderOrder(value =\"%04d\")\n", renderOrderHint))
			jw.WriteString(fmt.Sprintf("\t%s %s() throws Exception;\n\n", methodType, methodName))
			renderOrderHint = renderOrderHint + 1
		}

		jw.WriteString("\npublic interface " + className + " extends UnitBase {\n\n")
		for _, oc := range unit.Elements {
			if oc.ModelOnly == false {
				className := oc.GoType
				if len(oc.Class) > 0 {
					className = oc.Class
				}

				methodName := "get" + className
				if len(oc.InterfaceMethodName) > 0 {
				    methodName = oc.InterfaceMethodName
				}

				javaType := className
				if oc.List {
					javaType = "KubeList<" + className + ">"
					methodName = methodName + "List"
				} else if oc.Map {
					javaType = "Map<String," + className + ">"
					methodName = methodName + "Map"
				}
				writeMethodSig(javaType, methodName)
			}
		}

		jw.WriteString(fmt.Sprintf("\tinterface EZ extends %s {\n\n", className))
		for _, ezDefault := range ezDefaults {
			jw.WriteString(ezDefault)
		}
		jw.WriteString("\t}\n\n") // close EZ interface

		jw.WriteString("\n}\n")
		jw.Flush()
		javaFile.Close()
	}

	unitEnumWriter.WriteString("\t;\n\n") // end the enum element list

	unitEnumWriter.WriteString("\tpublic Class<?> mustImplementClass;\n\n")
	unitEnumWriter.WriteString("\tpublic String unitName;\n\n")
	unitEnumWriter.WriteString("\tpublic String humanName;\n\n")

	unitEnumWriter.WriteString("\tUnitType(Class<?> mustImplementClass, String unitName, String humanName) { this.mustImplementClass = mustImplementClass; this.unitName = unitName; this.humanName = humanName; }\n")

	unitEnumWriter.WriteString("\n}\n")
	unitEnumWriter.Flush()
	unitsEnumType.Close()

	return
}
