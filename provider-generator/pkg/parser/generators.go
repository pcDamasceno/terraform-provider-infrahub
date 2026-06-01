package parser

import (
	"bytes"
	"fmt"
	"html/template"
	"os"

	"github.com/marcom4rtinez/infrahub-terraform-provider-generator/pkg/emit"
	"github.com/marcom4rtinez/infrahub-terraform-provider-generator/pkg/queryir"
	"github.com/marcom4rtinez/infrahub-terraform-provider-generator/pkg/sdkintrospect"
	"github.com/marcom4rtinez/infrahub-terraform-provider-generator/pkg/templates"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

func ReadAndGenerateProvider(components TerraformComponents, providerDirectory string) {

	code, err := generateTerraformProvider(components)

	if err != nil {
		return
	}

	file, err := os.Create(fmt.Sprintf("%s/provider.go", providerDirectory))
	if err != nil {
		fmt.Println("Error creating the file:", err)
		return
	}
	defer file.Close()

	_, err = file.WriteString(code)
	if err != nil {
		fmt.Println("Error writing to the file:", err)
		return
	}

	fmt.Printf("Content written to provider.go file successfully!\n")
}

func ReadAndGenerateDataSourcesAndResources(graphqlQuery string, providerDirectory string, sdkDir string) (string, string, error) {

	parsedQuery, err := parseGraphQLQuery(graphqlQuery)

	if err != nil {
		fmt.Println("Error parsing GraphQL query:", err)
		os.Exit(1)
	}

	if parsedQuery.ResourceType == DataSource {
		q, err := queryir.Parse(graphqlQuery)
		if err != nil {
			fmt.Println("Error parsing GraphQL query:", err)
			os.Exit(1)
		}
		res, err := sdkintrospect.Load(sdkDir, q)
		if err != nil {
			fmt.Println("Error introspecting SDK:", err)
			os.Exit(1)
		}
		code, err := emit.DataSource(res)
		if err != nil {
			fmt.Println("Error emitting data source:", err)
			os.Exit(1)
		}
		file, err := os.Create(fmt.Sprintf("%s/%s_data_source.go", providerDirectory, q.BaseName))
		if err != nil {
			return "", "", err
		}
		defer file.Close()
		if _, err = file.WriteString(code); err != nil {
			return "", "", err
		}
		fmt.Printf("Content written to %s_data_source.go file successfully!\n", q.BaseName)
		return q.BaseName, "", nil
	} else if parsedQuery.ResourceType == Resource {
		code, err := generateTerraformResource(parsedQuery)
		if err != nil {
			return "", "", fmt.Errorf("Error generating Terraform resource: %s", err)
		}
		file, err := os.Create(fmt.Sprintf("%s/%s_resource.go", providerDirectory, parsedQuery.QueryName))
		if err != nil {
			return "", "", fmt.Errorf("Error creating the file: %s", err)
		}
		defer file.Close()

		_, err = file.WriteString(code)
		if err != nil {
			return "", "", fmt.Errorf("Error writing to the file: %s", err)
		}

		fmt.Printf("Content written to %s_resource.go file successfully!\n", parsedQuery.QueryName)
		return "", parsedQuery.QueryName, nil
	}

	return "", "", fmt.Errorf("No Resource or DataSource")

}

func generateTerraformProvider(components TerraformComponents) (string, error) {
	data := ProviderSourceTemplateData{
		DataSources: components.DataSources,
		Resources:   components.Resources,
	}

	// Render the template
	caser := cases.Title(language.English)
	providerTemplate, err := template.New("provider").Funcs(template.FuncMap{
		"title": caser.String,
	}).Parse(string(templates.ProviderTemplateContent))
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	err = providerTemplate.Execute(&buf, data)
	if err != nil {
		return "", err
	}

	return buf.String(), nil
}

func generateTerraformResource(parsedQuery *InputGraphQLQuery) (string, error) {
	structName := parsedQuery.QueryName + "Resource"
	data := ResourceTemplateData{
		QueryName:               parsedQuery.QueryName,
		ObjectName:              parsedQuery.ObjectName,
		Required:                parsedQuery.Required,
		StructName:              structName,
		Fields:                  parsedQuery.Fields,
		GenqlientFields:         parsedQuery.GenqlientFields,
		GenqlientFieldsModify:   parsedQuery.genqlientFieldsModify,
		GenqlientFieldsReadOnly: parsedQuery.genqlientFieldsReadOnly,
	}

	// Render the template
	caser := cases.Title(language.English)
	resourceTemplate, err := template.New("resource").Funcs(template.FuncMap{
		"title": caser.String,
	}).Parse(string(templates.ResourceTemplateContent))
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	err = resourceTemplate.Execute(&buf, data)
	if err != nil {
		return "", err
	}

	return buf.String(), nil
}

func GenerateArtifactDatasource(providerDirectory string) error {
	artifactTemplate, err := template.New("artifact").Parse(string(templates.ArtifactTemplateContent))
	if err != nil {
		return err
	}

	var buf bytes.Buffer
	err = artifactTemplate.Execute(&buf, "")
	if err != nil {
		return err
	}

	file, err := os.Create(fmt.Sprintf("%s/artifact_data_source.go", providerDirectory))
	if err != nil {
		return fmt.Errorf("Error creating the file: %s", err)
	}
	defer file.Close()

	_, err = file.WriteString(buf.String())
	if err != nil {
		return fmt.Errorf("Error writing to the file: %s", err)
	}

	fmt.Printf("Content written to provider.go file successfully!\n")
	return nil
}
