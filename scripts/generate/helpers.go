package main

import (
	"fmt"
	"strings"
	"text/template"
)

func generateHelpers(d APIDescription) error {
	helpers := strings.Builder{}
	helpers.WriteString(`
// THIS FILE IS AUTOGENERATED. DO NOT EDIT.
// Regen by running 'go generate' in the repo root.

package gotgbot

`)

	for _, tgMethodName := range orderedMethods(d) {
		tgMethod := d.Methods[tgMethodName]

		helper, err := generateHelperDef(d, tgMethod)
		if err != nil {
			return fmt.Errorf("failed to generate helpers for %s: %w", tgMethodName, err)
		}

		if helper == "" {
			continue
		}

		helpers.WriteString(helper)
	}

	return writeGenToFile(helpers, "gen_helpers.go")
}

func generateHelperDef(d APIDescription, tgMethod MethodDescription) (string, error) {
	helperDef := strings.Builder{}
	hasFromChat := false

	for _, x := range tgMethod.Fields {
		if x.Name == "from_chat_id" {
			hasFromChat = true
			break
		}
	}

	for _, typeName := range orderedTgTypes(d) {
		if typeName == tgTypeFile {
			continue
		}

		tgType := d.Types[typeName]

		newMethodName := strings.Replace(tgMethod.Name, typeName, "", 1)
		if newMethodName == tgMethod.Name {
			continue
		}

		fields := getMethodFieldsTypeMatches(tgMethod, typeName)
		if len(fields) == 0 {
			continue
		}

		newMethodName, err := getMethodFieldsSubtypeMatches(d, tgMethod, tgType, newMethodName, hasFromChat, fields)
		if err != nil {
			return "", err
		}

		newMethodName = strings.Title(newMethodName)

		ret, err := tgMethod.GetReturnTypes(d)
		if err != nil {
			return "", fmt.Errorf("failed to get return type for %s: %w", tgMethod.Name, err)
		}

		receiverName := tgType.receiverName()

		funcCallArgList, funcDefArgList, optsContent, err := generateHelperArguments(d, tgMethod, receiverName, fields)
		if err != nil {
			return "", err
		}

		funcDefArgs := strings.Join(funcDefArgList, ", ")
		funcCallArgs := strings.Join(funcCallArgList, ", ")

		helperDef.WriteString("\n// " + newMethodName + " Helper method for Bot." + strings.Title(tgMethod.Name))

		err = helperFuncTmpl.Execute(&helperDef, helperFuncData{
			Receiver:     receiverName,
			TypeName:     typeName,
			HelperName:   newMethodName,
			ReturnType:   strings.Join(ret, ", "),
			FuncDefArgs:  funcDefArgs,
			Contents:     optsContent,
			OptsName:     tgMethod.optsName(),
			MethodName:   strings.Title(tgMethod.Name),
			FuncCallArgs: funcCallArgs,
		})
		if err != nil {
			return "", fmt.Errorf("failed to execute template to generate %s helper method on %s: %w", newMethodName, typeName, err)
		}
	}

	return helperDef.String(), nil
}

func generateHelperArguments(d APIDescription, tgMethod MethodDescription, receiverName string, fields map[string]string) ([]string, []string, string, error) {
	var funcCallArgList []string
	optsContent := strings.Builder{}
	funcDefArgList := []string{"b *Bot"}
	hasOpts := false

	for _, mf := range tgMethod.Fields {
		hasOpts = hasOpts || !mf.Required

		prefType, err := mf.getPreferredType(d)
		if err != nil {
			return nil, nil, "", fmt.Errorf("failed to get preferred type for field %s of %s: %w", mf.Name, tgMethod.Name, err)
		}

		if fName, ok := fields[mf.Name]; ok {
			if !mf.Required {
				defaultValue := getDefaultTypeVal(d, prefType)
				optsContent.WriteString("\n	if opts." + snakeToTitle(mf.Name) + " == " + defaultValue + " {")
				if isPointer(prefType) {
					optsContent.WriteString("\n		opts." + snakeToTitle(mf.Name) + " = &" + receiverName + "." + snakeToTitle(fName))
				} else {
					optsContent.WriteString("\n		opts." + snakeToTitle(mf.Name) + " = " + receiverName + "." + snakeToTitle(fName))
				}
				optsContent.WriteString("\n	}")
				continue
			}

			funcCallArgList = append(funcCallArgList, receiverName+"."+snakeToTitle(fName))
			continue
		}

		if !mf.Required {
			continue
		}

		funcDefArgList = append(funcDefArgList, snakeToCamel(mf.Name)+" "+prefType)
		funcCallArgList = append(funcCallArgList, snakeToCamel(mf.Name))
	}

	funcDefArgList = append(funcDefArgList, "opts *"+tgMethod.optsName())
	funcCallArgList = append(funcCallArgList, "opts")

	return funcCallArgList, funcDefArgList, optsContent.String(), nil
}

func getMethodFieldsSubtypeMatches(d APIDescription, tgMethod MethodDescription, tgType TypeDescription, repl string, hasFromChat bool, fields map[string]string) (string, error) {
	for _, f := range tgType.Fields {
		if f.Name == "reply_to_message" {
			// this subfield just causes confusion; we always want the message_id
			continue
		}

		for _, mf := range tgMethod.Fields {
			prefType, err := f.getPreferredType(d)
			if err != nil {
				return "", fmt.Errorf("failed to get preferred type for field %s of %s: %w", mf.Name, tgMethod.Name, err)
			}

			if isTgType(d, prefType) && f.Name+"_id" == mf.Name {
				repl = strings.ReplaceAll(repl, prefType, "")

				if hasFromChat && mf.Name == "chat_id" {
					fields["from_chat_id"] = f.Name + ".Id"
				} else {
					fields[mf.Name] = f.Name + ".Id" // Note: maybe not just assume ID field exists?
				}
			}
		}
	}
	return repl, nil
}

func getMethodFieldsTypeMatches(tgMethod MethodDescription, typeName string) map[string]string {
	fields := map[string]string{}
	for _, f := range tgMethod.Fields {
		if f.Name == titleToSnake(typeName)+"_id" || f.Name == "id" {
			idField := "id"

			if typeName == tgTypeMessage {
				idField = "message_id"
			} else if typeName == tgTypeFile {
				idField = "file_id"
			}

			fields[titleToSnake(typeName)+"_id"] = idField
		}
	}
	return fields
}

var helperFuncTmpl = template.Must(template.New("helperFunc").Parse(helperFunc))

type helperFuncData struct {
	Receiver     string
	TypeName     string
	HelperName   string
	ReturnType   string
	FuncDefArgs  string
	Contents     string
	OptsName     string
	MethodName   string
	FuncCallArgs string
}

const helperFunc = `
func ({{.Receiver}} {{.TypeName}}) {{.HelperName}}({{.FuncDefArgs}}) ({{.ReturnType}}, error) {
	{{- if .Contents}}
		if opts == nil {
			opts = &{{.OptsName}}{}
		}
		{{.Contents}}

	{{end}}
	return b.{{.MethodName}}({{.FuncCallArgs}})
}
`
