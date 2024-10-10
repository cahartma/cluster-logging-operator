package matchers

import (
	"fmt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"reflect"
	"regexp"
	"strings"
	"time"

	log "github.com/ViaQ/logerr/v2/log/static"
	"github.com/onsi/gomega/types"
	"github.com/openshift/cluster-logging-operator/test"
	testtypes "github.com/openshift/cluster-logging-operator/test/helpers/types"
)

type LogMatcher struct {
	expected interface{}
	field    string
}

func FitLogFormatTemplate(expected interface{}) types.GomegaMatcher {
	return &LogMatcher{
		expected: expected,
	}
}

func (m *LogMatcher) Match(actual interface{}) (success bool, err error) {
	if reflect.TypeOf(m.expected) != reflect.TypeOf(actual) {
		return false, fmt.Errorf("matcher expects to compare same log types")
	}

	m.field, success, err = CompareLog(m.expected, actual)
	return success, err
}

func (m *LogMatcher) FailureMessage(actual interface{}) (message string) {
	return fmt.Sprintf("Expected\n\t%s\nto fit \n\t%s\nFailed field is: %s", test.JSONString(actual), test.JSONString(m.expected), m.field)
}

func (m *LogMatcher) NegatedFailureMessage(actual interface{}) (message string) {
	return fmt.Sprintf("Expected\n\t%s\nto not fit \n\t%s\nFailed field is: %s", test.JSONString(actual), test.JSONString(m.expected), m.field)
}

func isNil(i interface{}) bool {
	if i == nil {
		return true
	}
	switch reflect.TypeOf(i).Kind() {
	case reflect.Ptr, reflect.Map, reflect.Array, reflect.Chan, reflect.Slice:
		return reflect.ValueOf(i).IsNil()
	}
	return false
}

func DeepFields(iface interface{}, namePrefix string) ([]reflect.Value, []string) {
	values := make([]reflect.Value, 0)
	names := make([]string, 0)

	ifv := reflect.ValueOf(iface)
	ift := reflect.TypeOf(iface)
	log.V(3).Info("Evaluating deep fields", "type", ift.Name(), "interface", test.JSONLine(iface))

	for i := 0; i < ift.NumField(); i++ {
		v := ifv.Field(i)
		n := namePrefix + ifv.Type().Field(i).Name
		log.V(3).Info("deep field", "fieldName", n, "kind", v.Kind().String())
		if !v.CanInterface() {
			continue
		}
		switch v.Kind() {
		case reflect.Array:
			values = append(values, v)
			names = append(names, n)
		case reflect.Struct:
			typename := v.Type().Name()
			if typename == "Timing" {
				break
			}
			if typename != "Time" {
				moreFields, moreNames := DeepFields(v.Interface(), n+"_")
				values = append(values, moreFields...)
				names = append(names, moreNames...)
			} else {
				values = append(values, v)
				names = append(names, n)
			}
		case reflect.Ptr:
			if !isNil(v.Interface()) {
				elm := v.Elem()
				moreFields, moreNames := DeepFields(elm.Interface(), n+"_")
				values = append(values, moreFields...)
				names = append(names, moreNames...)
			}
		default:
			values = append(values, v)
			names = append(names, n)
		}
	}

	return values, names
}

func compareLogLogic(name string, templateValue interface{}, value interface{}) bool {
	templateValueString := fmt.Sprintf("%v", templateValue)
	valueString := fmt.Sprintf("%v", value)

	templateType := reflect.TypeOf(templateValue)
	if templateType.Name() == "OptionalInt" {
		expValue := templateValue.(testtypes.OptionalInt)
		actValue := value.(testtypes.OptionalInt)
		log.V(3).Info("CompareLogLogic: OptionalInt for", "name", name, "value", valueString, "exp", expValue, "act", actValue)
		return expValue.IsSatisfiedBy(actValue)
	}
	if templateValueString == valueString { // Same value is ok
		log.V(3).Info("CompareLogLogic: Same value for", "name", name, "value", valueString)
		return true
	}
	if templateValueString == "**optional**" {
		log.V(3).Info("CompareLogLogic: Optional value for **optional** ", "fieldname", name, "value", value)
		return true
	}
	if templateValueString == "*" && valueString != "" { // Any value, not Nil is ok if template value is "*"
		log.V(3).Info("CompareLogLogic: Any value for * ", "fieldname", name, "value", value)
		return true
	}
	if templateValueString == "[*]" && valueString != "" { // Any array
		log.V(3).Info("CompareLogLogic: Any value for array[*] ", "fieldname", name, "value", value)
		return true
	}

	if templateValueString == "map[*:*]" && valueString != "" { // Any map
		log.V(3).Info("CompareLogLogic: Any value for map[*] ", "fieldname", name, "value", value)
		return true
	}
	if templateValueString == "[]" && valueString != "[]" { // Any value, not Nil is ok if template value is an array "[*]"
		log.V(3).Info("CompareLogLogic: Any value for * ", "name", name, "value", valueString)
		return true
	}
	if templateValueString == "0" && valueString != "" { // Any value, not Nil is ok if template value is an array "[*]"
		log.V(3).Info("CompareLogLogic: Any value for * ", "name", name, "value", valueString)
		return true
	}
	if templateType.Name() == "Time" {

		var templateTime time.Time
		var valueTime time.Time
		switch templateType.PkgPath() {
		case "time":
			templateTime = templateValue.(time.Time)
			valueTime = value.(time.Time)
		case "k8s.io/apimachinery/pkg/apis/meta/v1":
			templateTime = templateValue.(metav1.Time).Time
			valueTime = value.(metav1.Time).Time
		default:
			log.V(0).Info("Unable to compare unsupported Time type", "pkg", templateType.PkgPath())
			return false
		}

		if templateTime.UTC() == valueTime.UTC() {
			return true
		}

		// Any time value not Nil is ok if template value is empty time and also does not equal the value for time.Time{}
		if templateValueString == "0001-01-01 00:00:00 +0000 UTC" && valueString != "" && valueString != "0001-01-01 00:00:00 +0000 UTC" {
			log.V(3).Info("CompareLogLogic: Any value for 'empty time' ", "name", name, "value", valueString)
			return true
		}
	}

	if strings.HasPrefix(templateValueString, "regex:") { // Using regex if starts with "/"
		match, _ := regexp.MatchString(templateValueString[6:], valueString)
		if match {
			log.V(3).Info("CompareLogLogic: Fit regex ", "fieldname", name, "value", value)
			return true
		}
	}

	log.V(3).Info("CompareLogLogic: Mismatch !!!", "fieldname", name, "templateValue", test.JSONLine(templateValue), "value", test.JSONLine(value))
	return false
}

func CompareLog(template interface{}, actual interface{}) (string, bool, error) {
	logFieldValues, logFieldNames := DeepFields(actual, "")

	// templateString := test.JSONLine(template)
	// logger.V(3).Info("Marshalled", "template", templateString)
	// allLog := &logtypes.AllLog{}
	// test.MustUnmarshal(templateString, allLog)
	// logger.V(3).Info("Unmarshled", "template", template)
	templateFieldValues, templateFieldNames := DeepFields(template, "")
	log.V(3).Info("Template", "names", templateFieldNames)
	for i := range templateFieldNames {
		templateFieldValue := templateFieldValues[i].Interface()
		templateFieldName := templateFieldNames[i]
		foundMatchFields := false
		for j := range logFieldValues {
			logFieldValue := logFieldValues[j].Interface()
			logFieldName := logFieldNames[j]
			if templateFieldName == logFieldName {
				foundMatchFields = true
				log.V(3).Info("CompareLog: comparing", "name", templateFieldName)
				if !isNil(templateFieldValue) { // Are we interested this field?
					if templateFieldValues[j].Kind() == reflect.Ptr { // Skip skeleton structure fields
						log.V(3).Info("CompareLog: skipping skeleton", "name", templateFieldName)
						break
					}

					if compareLogLogic(templateFieldName, templateFieldValue, logFieldValue) {
						break
					}
					return templateFieldName, false, nil
				} else {
					log.V(3).Info("CompareLog: skipping not interesting field", "name", templateFieldName)
					break // If this is not an interesting field
				}
			}
		}
		if !foundMatchFields {
			log.V(3).Info("CompareLog: skipping field, not found in log", "name", templateFieldName)
		}
	}

	return "", true, nil
}
