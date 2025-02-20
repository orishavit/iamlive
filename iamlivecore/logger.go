package iamlivecore

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"github.com/kenshaw/baseconv"
	"github.com/otterize/iamlive/iamlivecore/mapperclient"
	"github.com/otterize/nilable"
	"log"
	"net/url"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

//go:embed map.json
var bIAMMap []byte

//go:embed iam_definition.json
var bIAMSAR []byte

var callLog []Entry

type AzureEntry struct {
	HTTPMethod string
	Path       string
	Parameters map[string][]string
	Body       []byte
}

// JSON maps
var iamMap iamMapBase
var iamDef []iamDefService

// Entry is a single CSM entry
type Entry struct {
	Region              string `json:"Region"`
	Type                string `json:"Type"`
	Service             string `json:"Service"`
	Method              string `json:"Api"`
	Parameters          map[string][]string
	URIParameters       map[string]string
	FinalHTTPStatusCode int    `json:"FinalHttpStatusCode"`
	AccessKey           string `json:"AccessKey"`
	SrcIP               string `json:"SrcIp"`
	ClientIdentity      *NamespacedName
}

// Statement is a single statement within an IAM policy
type Statement struct {
	Effect   string   `json:"Effect"`
	Action   []string `json:"Action"`
	Resource []string `json:"Resource"`
}

// IAMPolicy is a full IAM policy
type IAMPolicy struct {
	Version   string      `json:"Version"`
	Statement []Statement `json:"Statement"`
}

type AzureIAMPolicy struct {
	Name             string   `json:"Name"`
	IsCustom         bool     `json:"IsCustom"`
	Description      string   `json:"Description"`
	Actions          []string `json:"Actions"`
	DataActions      []string `json:"DataActions"`
	NotDataActions   []string `json:"NotDataActions"`
	AssignableScopes []string `json:"AssignableScopes"`
}

func LoadMaps() {
	if *providerFlag == "aws" {
		err := json.Unmarshal(bIAMMap, &iamMap)

		if err != nil {
			log.Fatal(err)
		}

		err = json.Unmarshal(bIAMSAR, &iamDef)

		if err != nil {
			panic(err)
		}
	}
}

func ClearLog() {
	callLog = []Entry{}
}

func GetPolicyDocument() []byte {
	if *providerFlag == "aws" {
		policy := IAMPolicy{
			Version:   "2012-10-17",
			Statement: []Statement{},
		}

		if *modeFlag == "proxy" {
			for _, entry := range callLog {
				if *failsonlyFlag && (entry.FinalHTTPStatusCode >= 200 && entry.FinalHTTPStatusCode <= 299) {
					continue
				}

				policy.Statement = append(policy.Statement, getStatementsForProxyCall(entry)...)
			}

			if *forceWildcardResourceFlag {
				for i, _ := range policy.Statement {
					policy.Statement[i].Resource = []string{"*"}
				}
			}

			policy = aggregatePolicy(policy)
		}

		doc, err := json.MarshalIndent(policy, "", "    ")
		if err != nil {
			panic(err)
		}
		return doc
	}

	return []byte("ERROR")
}

func removeStatementItem(slice []Statement, i int) []Statement {
	copy(slice[i:], slice[i+1:])
	return slice[:len(slice)-1]
}

func aggregatePolicy(policy IAMPolicy) IAMPolicy {
	for i := 0; i < len(policy.Statement); i++ {
		sort.Strings(policy.Statement[i].Resource)
		for j := i + 1; j < len(policy.Statement); j++ {
			sort.Strings(policy.Statement[j].Resource)

			if reflect.DeepEqual(policy.Statement[i].Resource, policy.Statement[j].Resource) {
				policy.Statement[i].Action = append(policy.Statement[i].Action, policy.Statement[j].Action...) // combine
				policy.Statement = removeStatementItem(policy.Statement, j)                                    // remove dupe
				j--
			}
		}

		actions := uniqueSlice(policy.Statement[i].Action)

		if *sortAlphabeticalFlag {
			sort.Strings(actions)
		}

		policy.Statement[i].Action = actions
		policy.Statement[i].Resource = uniqueSlice(policy.Statement[i].Resource)
	}

	return policy
}

func printCallInfo(entry Entry) {
	statements := getStatementsForProxyCall(entry)
	graphQLClient := mapperclient.NewClient("http://otterize-network-mapper.otterize-system.svc.cluster.local:9090")
	var operations []mapperclient.AWSOperation

	for _, statement := range statements {
		for _, resource := range statement.Resource {
			operation := mapperclient.AWSOperation{
				Resource: resource,
				Actions:  statement.Action,
				SrcIp:    nilable.From(entry.SrcIP),
			}

			if entry.ClientIdentity != nil {
				operation.Client = nilable.From(mapperclient.NamespacedName{
					Namespace: entry.ClientIdentity.Namespace,
					Name:      entry.ClientIdentity.Name,
				})
			}

			operations = append(operations, operation)
		}
	}

	_ = graphQLClient.ReportAWSOperation(context.Background(), operations)
}

type iamMapMethod struct {
	Action              string                      `json:"action"`
	ResourceMappings    map[string]iamMapResMapItem `json:"resource_mappings"`
	ResourceARNMappings map[string]string           `json:"resourcearn_mappings"`
	ArnOverride         iamMapArnOverride           `json:"arn_override"`
}

type iamMapArnOverride struct {
	Template string `json:"template"`
}

type iamMapResMapItem struct {
	Template string `json:"template"`
}

type iamMapBase struct {
	SDKMethodIAMMappings     map[string][]iamMapMethod `json:"sdk_method_iam_mappings"`
	SDKServiceMappings       map[string]string         `json:"sdk_service_mappings"`
	SDKPermissionlessActions []string                  `json:"sdk_permissionless_actions"`
}

type iamDefService struct {
	Prefix     string            `json:"prefix"`
	Privileges []iamDefPrivilege `json:"privileges"`
	Resources  []iamDefResource  `json:"resources"`
}

type iamDefPrivilege struct {
	Privilege     string               `json:"privilege"`
	ResourceTypes []iamDefResourceType `json:"resource_types"`
	Description   string               `json:"description"`
}

type iamDefResource struct {
	Resource string `json:"resource"`
	Arn      string `json:"arn"`
}

type iamDefResourceType struct {
	DependentActions []string `json:"dependent_actions"`
	ResourceType     string   `json:"resource_type"`
}

func uniqueSlice(slice []string) []string {
	keys := make(map[string]bool)
	list := []string{}
	for _, entry := range slice {
		if _, value := keys[entry]; !value {
			keys[entry] = true
			list = append(list, entry)
		}
	}
	return list
}

func getDependantActions(actions []string) []string {
	for _, baseaction := range actions {
		splitbase := strings.Split(baseaction, ":")
		if len(splitbase) != 2 {
			continue
		}
		baseservice := splitbase[0]
		basemethod := splitbase[1]

		for _, service := range iamDef {
			if strings.ToLower(service.Prefix) == strings.ToLower(baseservice) {
				for _, priv := range service.Privileges {
					if strings.ToLower(priv.Privilege) == strings.ToLower(basemethod) {
						for _, resourceType := range priv.ResourceTypes {
							for _, dependentAction := range resourceType.DependentActions {
								actions = append(actions, dependentAction)
							}
						}
					}
				}
			}
		}
	}

	return uniqueSlice(actions)
}

func getActions(service, method string) []string {
	var actions []string

	// checked if permissionless
	for _, permissionlessAction := range iamMap.SDKPermissionlessActions {
		if strings.ToLower(permissionlessAction) == fmt.Sprintf("%s.%s", strings.ToLower(service), strings.ToLower(method)) {
			return []string{}
		}
	}

	// check IAM mappings
	for sdkCall, mappingInfo := range iamMap.SDKMethodIAMMappings {
		if fmt.Sprintf("%s.%s", strings.ToLower(service), strings.ToLower(method)) == strings.ToLower(sdkCall) {
			for _, item := range mappingInfo {
				actions = append(actions, item.Action)
			}
		}
	}

	if len(actions) > 0 {
		return actions
	}

	// substitute service name
	for sdkService, iamService := range iamMap.SDKServiceMappings {
		if service == sdkService {
			service = iamService
			break
		}
	}

	return []string{
		fmt.Sprintf("%s:%s", strings.ToLower(service), method),
	}
}

func resolveSpecials(arn string, call Entry, mandatory bool, resourceArnTemplate *string) []string {
	startIndex := strings.Index(arn, "%%")
	endIndex := strings.LastIndex(arn, "%%")

	if startIndex > -1 && endIndex != startIndex {
		parts := strings.Split(arn[startIndex+2:endIndex], "%")

		if len(parts) < 2 {
			return []string{arn[0:startIndex] + "*" + arn[endIndex+2:]}
		}

		switch parts[0] {
		case "iftruthy":
			if len(parts) == 3 { // weird bug for empty string false values
				parts = append(parts, "")
			}

			if len(parts) != 4 {
				return []string{arn[0:startIndex] + "*" + arn[endIndex+2:]}
			}

			fullyResolved, arns := subARNParameters(parts[1], call, true)

			if len(arns) < 1 || arns[0] == "" || !fullyResolved {
				if parts[3] == "" {
					if mandatory {
						return []string{arn[0:startIndex] + "*" + arn[endIndex+2:]}
					}
					return []string{arn[0:startIndex] + arn[endIndex+2:]}
				}
				return []string{arn[0:startIndex] + parts[3] + arn[endIndex+2:]}
			}

			if parts[2] == "" && mandatory {
				if mandatory {
					return []string{arn[0:startIndex] + "*" + arn[endIndex+2:]}
				}
				return []string{arn[0:startIndex] + arn[endIndex+2:]}
			}
			return []string{arn[0:startIndex] + parts[2] + arn[endIndex+2:]}
		case "urlencode":
			if len(parts) != 2 {
				return []string{arn[0:startIndex] + "*" + arn[endIndex+2:]}
			}

			fullyResolved, arns := subARNParameters(parts[1], call, true)
			if len(arns) < 1 || arns[0] == "" || !fullyResolved {
				if mandatory {
					return []string{arn[0:startIndex] + "*" + arn[endIndex+2:]}
				}
				return []string{arn[0:startIndex] + arn[endIndex+2:]}
			}

			return []string{arn[0:startIndex] + url.QueryEscape(arns[0]) + arn[endIndex+2:]}
		case "iftemplatematch":
			if len(parts) != 2 || resourceArnTemplate == nil {
				return []string{arn[0:startIndex] + "*" + arn[endIndex+2:]}
			}

			fullyResolved, arns := subARNParameters(parts[1], call, true)
			if len(arns) < 1 || arns[0] == "" || !fullyResolved {
				return []string{arn[0:startIndex] + arn[endIndex+2:]}
			}

			template := regexp.MustCompile(`\\\$\\\{.+?\\\}`).ReplaceAllString(regexp.QuoteMeta(*resourceArnTemplate), ".*?")

			if regexp.MustCompile(template).MatchString(arns[0]) {
				return []string{arn[0:startIndex] + arns[0] + arn[endIndex+2:]}
			}

			return []string{arn[0:startIndex] + arn[endIndex+2:]}
		case "many":
			manyParts := []string{}

			for _, part := range parts[1:] {
				fullyResolved, arns := subARNParameters(part, call, true)
				if len(arns) < 1 || arns[0] == "" || !fullyResolved {
					if mandatory {
						return []string{arn[0:startIndex] + "*" + arn[endIndex+2:]}
					}
					return []string{arn[0:startIndex] + arn[endIndex+2:]}
				}

				manyParts = append(manyParts, arns[0])
			}

			return manyParts
		case "regex":
			if len(parts) != 3 {
				return []string{arn[0:startIndex] + "*" + arn[endIndex+2:]}
			}

			fullyResolved, arns := subARNParameters(parts[1], call, true)

			if len(arns) < 1 || arns[0] == "" || !fullyResolved {
				if mandatory {
					return []string{arn[0:startIndex] + "*" + arn[endIndex+2:]}
				}
				return []string{arn[0:startIndex] + arn[endIndex+2:]}
			}

			if parts[2][0] == '/' {
				parts[2] = parts[2][1 : len(parts[2])-2]
			}

			r := regexp.MustCompile(parts[2])
			groups := r.FindStringSubmatch(strings.ReplaceAll(arns[0], `$`, `$$`))

			if len(groups) < 2 || groups[1] == "" {
				if mandatory {
					return []string{arn[0:startIndex] + "*" + arn[endIndex+2:]}
				}
				return []string{arn[0:startIndex] + arn[endIndex+2:]}
			}

			return []string{arn[0:startIndex] + groups[1] + arn[endIndex+2:]}
		default: // unknown function
			return []string{arn[0:startIndex] + "*" + arn[endIndex+2:]}
		}
	}

	return []string{arn}
}

func getStatementsForProxyCall(call Entry) (statements []Statement) {
	lowerPriv := strings.ToLower(fmt.Sprintf("%s.%s", call.Service, call.Method))

	for iamMapMethodName, iamMapMethods := range iamMap.SDKMethodIAMMappings {
		if strings.ToLower(iamMapMethodName) == lowerPriv {
			for mappedPrivIndex, mappedPriv := range iamMapMethods {
				resources := []string{}

				// arn_override
				if mappedPriv.ArnOverride.Template != "" {
					arns := resolveSpecials(mappedPriv.ArnOverride.Template, call, false, nil)

					if len(arns) == 0 || len(arns) > 1 || arns[0] != "" { // skip if empty after resolving specials
						for _, arn := range arns {
							fullyResolved, subbedArns := subARNParameters(arn, call, false)
							for _, subbedArn := range subbedArns {
								if mappedPrivIndex == 0 || fullyResolved {
									resources = append(resources, subbedArn) // sub full parameters and add to resources
								}
							}
						}
					}

					if len(resources) == 0 && len(mappedPriv.ResourceMappings) == 0 {
						continue
					}
				}

				// resourcearn_mappings
				if len(mappedPriv.ResourceARNMappings) > 0 {
					for _, service := range iamDef { // in the SAR
						if service.Prefix == strings.ToLower(strings.Split(mappedPriv.Action, ":")[0]) { // find the service for the call
							for _, servicePrivilege := range service.Privileges {
								if strings.ToLower(strings.Split(mappedPriv.Action, ":")[1]) == strings.ToLower(servicePrivilege.Privilege) { // find the method for the call
									for _, resourceType := range servicePrivilege.ResourceTypes { // get all resource types for the privilege
										resourceArnTemplate := ""
										for _, resource := range service.Resources { // go through the service resources
											if resource.Resource == strings.Replace(resourceType.ResourceType, "*", "", -1) && resource.Resource != "" { // match the resource type (doesn't matter if mandatory)
												resourceArnTemplate = resource.Arn
											}
										}
										for mapResType, mapResTemplate := range mappedPriv.ResourceARNMappings {
											if strings.Replace(resourceType.ResourceType, "*", "", -1) == mapResType {
												mandatory := strings.HasSuffix(resourceType.ResourceType, "*")

												resARNMappingTemplates := resolveSpecials(mapResTemplate, call, false, &resourceArnTemplate)
												if len(resARNMappingTemplates) == 1 && resARNMappingTemplates[0] == "" {
													continue
												}

												if len(resARNMappingTemplates) == 0 && mandatory && len(mappedPriv.ResourceMappings) == 0 {
													resARNMappingTemplates = []string{"*"}
												}

												for _, resARNMappingTemplate := range resARNMappingTemplates {
													fullyResolved, subbedArns := subARNParameters(resARNMappingTemplate, call, false)
													if mandatory || fullyResolved { // check if mandatory or fully resolved
														resources = append(resources, subbedArns...) // sub full parameters and add to resources
													}
												}
											}
										}
									}
								}
							}
						}
					}
				}

				// resource_mappings
				if len(resources) == 0 {
					for _, service := range iamDef { // in the SAR
						if service.Prefix == strings.ToLower(strings.Split(mappedPriv.Action, ":")[0]) { // find the service for the call
							for _, servicePrivilege := range service.Privileges {
								if strings.ToLower(strings.Split(mappedPriv.Action, ":")[1]) == strings.ToLower(servicePrivilege.Privilege) { // find the method for the call
									for _, resourceType := range servicePrivilege.ResourceTypes { // get all resource types for the privilege
										for _, resource := range service.Resources { // go through the service resources
											if resource.Resource == strings.Replace(resourceType.ResourceType, "*", "", -1) && resource.Resource != "" { // match the resource type (doesn't matter if mandatory)
												arns := []string{resource.Arn} // the base ARN template, matrix init
												newArns := []string{}
												mandatory := strings.HasSuffix(resourceType.ResourceType, "*")

												// substitute the resource_mappings
												for resMappingVar, resMapping := range mappedPriv.ResourceMappings { // for each mapping
													resMappingTemplates := resolveSpecials(resMapping.Template, call, false, &resource.Arn) // get a list of resolved template strings

													if len(resMappingTemplates) == 1 && resMappingTemplates[0] == "" {
														continue
													}

													for _, arn := range arns { // for each of the arn list
														newArns = []string{}

														for _, resMappingTemplate := range resMappingTemplates {
															variableReplaced := regexp.MustCompile(`\$\{`+resMappingVar+`\}`).ReplaceAllString(arn, strings.ReplaceAll(resMappingTemplate, `$`, `$$`)) // escape $ for regexp
															newArns = append(newArns, variableReplaced)
														}
													}
													arns = newArns
												}

												if len(arns) == 0 && mandatory {
													arns = []string{"*"}
												}

												for _, arn := range arns {
													fullyResolved, subbedArns := subARNParameters(arn, call, false)
													if mandatory || fullyResolved { // check if mandatory or fully resolved
														resources = append(resources, subbedArns...) // sub full parameters and add to resources
													}
												}
											}
										}
									}
								}
							}
						}
					}
				}

				// default (last ditch)
				if len(resources) == 0 {
					if len(mappedPriv.ResourceARNMappings) > 0 { // skip if resourcearn_mapping was specified and didn't hit
						continue
					}
					resources = []string{"*"}
				}

				statements = append(statements, Statement{
					Effect:   "Allow",
					Resource: resources,
					Action:   []string{mappedPriv.Action},
				})
			}
		}
	}

	return statements
}

func getAccountFromAccessKey(accessKeyId string) (string, error) {
	base10 := "0123456789"
	base32AwsFlavour := "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"

	offsetStr, err := baseconv.Convert("QAAAAAAA", base32AwsFlavour, base10)
	if err != nil {
		return "", err
	}
	offset, err := strconv.Atoi(offsetStr)
	if err != nil {
		return "", err
	}

	offsetAccountIdStr, err := baseconv.Convert(accessKeyId[4:12], base32AwsFlavour, base10)
	if err != nil {
		return "", err
	}
	offsetAccountId, err := strconv.Atoi(offsetAccountIdStr)
	if err != nil {
		return "", err
	}

	accountId := 2 * (offsetAccountId - offset)

	if strings.Index(base32AwsFlavour, accessKeyId[12:13]) >= strings.Index(base32AwsFlavour, "Q") {
		accountId++
	}

	if accountId < 0 {
		return "", fmt.Errorf("negative account ID")
	}

	return fmt.Sprintf("%012d", accountId), nil
}

type uniqueStringList struct {
	list []string
	set  map[string]bool
}

func newUniqueStringList() *uniqueStringList {
	return &uniqueStringList{set: map[string]bool{}}
}
func (s *uniqueStringList) add(newArn string) {
	if _, ok := s.set[newArn]; !ok {
		s.list = append(s.list, newArn)
		s.set[newArn] = true
	}
}

func (s *uniqueStringList) addParam(arns []string, paramVarName, param string) {
	for _, arn := range arns {
		newArn := regexp.MustCompile(`\$\{`+strings.ReplaceAll(strings.ReplaceAll(paramVarName, "[", "\\["), "]", "\\]")+`\}`).ReplaceAllString(arn, param)
		s.add(newArn)
	}
}

func subARNParameters(arn string, call Entry, specialsOnly bool) (bool, []string) {
	arns := []string{arn}
	// parameter substitution
	for paramVarName, params := range call.Parameters {
		newArns := newUniqueStringList()
		for _, param := range params {
			newArns.addParam(arns, paramVarName, param)
		}
		arns = newArns.list
	}

	// URI parameter substitution
	for paramVarName, param := range call.URIParameters {
		newArns := newUniqueStringList()
		newArns.addParam(arns, paramVarName, param)
		arns = newArns.list
	}

	if specialsOnly {
		anyMatched := false
		for _, arn := range arns {
			matched, _ := regexp.Match(`\$\{.+?\}`, []byte(arn))
			if matched {
				anyMatched = true
			}
		}

		return !anyMatched, arns
	}

	account := *accountIDFlag
	var err error

	if account == "" && call.AccessKey != "" {
		account, err = getAccountFromAccessKey(call.AccessKey)
		if err != nil || account == "" {
			account = "123456789012"
		}
	}

	partition := "aws"
	if strings.HasPrefix(call.Region, "cn") {
		partition = "aws-cn"
	}
	if strings.HasPrefix(call.Region, "us-gov") {
		partition = "aws-us-gov"
	}

	anyUnresolved := false
	result := []string{}
	for _, arn := range arns {
		arn = regexp.MustCompile(`\$\{Partition\}`).ReplaceAllString(arn, partition)
		arn = regexp.MustCompile(`\$\{Region\}`).ReplaceAllString(arn, call.Region)
		arn = regexp.MustCompile(`\$\{Account\}`).ReplaceAllString(arn, account)
		unresolvedArn := arn
		arn = regexp.MustCompile(`\$\{.+?\}`).ReplaceAllString(arn, "*") // TODO: preserve ${aws:*} variables
		if unresolvedArn != arn {
			anyUnresolved = true
		}
		result = append(result, arn)
	}

	return !anyUnresolved, result
}

func mapServicePrefix(prefix string, mappings iamMapBase) string {
	for sdkprefix, mappedprefix := range mappings.SDKServiceMappings {
		if sdkprefix == prefix {
			return mappedprefix
		}
	}

	return prefix
}
