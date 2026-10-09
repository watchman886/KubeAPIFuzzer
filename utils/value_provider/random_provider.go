package value_provider

import (
	"fmt"
	"k-fuzz/utils"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/sirupsen/logrus"
)

// RandomProvider implements ValueProvider using a pseudo-random strategy.
type RandomProvider struct {
	resourceKind string
	apiVersion   string
	maxDepth     uint
}

// NewRandomProvider returns a new RandomProvider initialized with the given seed.
func NewRandomProvider(kind string, apiVersion string, maxDepth uint) ValueProvider {
	return &RandomProvider{
		resourceKind: kind,
		apiVersion:   apiVersion,
		maxDepth:     maxDepth,
	}
}

// FillSchema returns a randomly generated value that matches the given schema.
// - Strings: either a random enum entry or a random string of length between MinLength and MaxLength.
// - Numbers: a random float between Min and Max.
// - Booleans: true or false at random.
// - Arrays: a slice of random length between MinItems and MaxItems.
// - Objects: a map whose keys are property names and whose values are recursively generated.
func (p *RandomProvider) FillSchema(ref *openapi3.SchemaRef, propertyName string, depth uint) any {
	if depth > p.maxDepth || ref == nil || ref.Value == nil {
		return nil
	}
	s := ref.Value

	if special, ok := specialObjectValue(p.resourceKind, p.apiVersion, propertyName); ok {
		return special
	}

	typ := schemaType(ref)

	// Handle oneOf/anyOf if type is still unknown
	if typ == "" {
		if len(s.OneOf) > 0 {
			return p.FillSchema(s.OneOf[utils.R.IntN(len(s.OneOf))], propertyName, depth+1)
		}
		if len(s.AnyOf) > 0 {
			return p.FillSchema(s.AnyOf[utils.R.IntN(len(s.AnyOf))], propertyName, depth+1)
		}
		if len(s.AllOf) > 0 {
			return p.FillSchema(s.AllOf[utils.R.IntN(len(s.AllOf))], propertyName, depth+1)
		}

		switch ref.Ref {
		case "#/components/schemas/io.k8s.apiextensions-apiserver.pkg.apis.apiextensions.v1.JSON":
			return fixedValue.Json
		}

		logrus.Warnf("schema type is nil, and met unexpected ref: %s", ref.Ref)
		return fixedValue.String
	}

	switch typ {
	case "string":
		if formatted, ok := formattedStringValue(s.Format); ok {
			return formatted
		}
		return p.FillString(convertAnySliceToStringSlice(s.Enum), s.MinLength, s.MaxLength)

	case "integer":
		switch s.Format {
		case "double":
			return p.FillDouble(s.Min, s.Max)
		default:
			return p.FillInteger(s.Min, s.Max)
		}

	case "boolean":
		return p.FillBoolean()

	case "array":
		minLen := intOrDefault(s.MinItems, 1)
		maxLen := intOrDefault(s.MaxItems, 5)
		length := utils.R.IntN(maxLen-minLen+1) + minLen
		arr := make([]any, length)
		for i := range arr {
			arr[i] = p.FillSchema(s.Items, propertyName, depth+1)
		}
		return arr

	case "object":
		selectedNames := objectPropertyNames(s, true)
		obj := make(map[string]any, len(selectedNames))
		for _, name := range selectedNames {
			if prop, ok := s.Properties[name]; ok {
				obj[name] = p.FillSchema(prop, name, depth+1)
			}
		}
		return obj

	default:
		return nil
	}
}

// FillParameter only fills parameters, does not consider "required" field
func (p *RandomProvider) FillParameter(ref *openapi3.ParameterRef) string {
	if ref == nil || ref.Value == nil {
		return utils.StringRefMissingFallback
	}
	if val, ok := preferredParameterValue(ref.Value.Name); ok {
		return val
	}

	s := ref.Value.Schema
	if s == nil || s.Value == nil {
		return utils.StringRefMissingFallback
	}

	val := p.FillSchema(s, ref.Value.Name, 0)
	return fmt.Sprint(val)
}

// ContentType picks one media type at random.
func (p *RandomProvider) ContentType(content openapi3.Content) string {
	mediaTypes := preferredContentTypes(content)
	if len(mediaTypes) == 0 {
		return ""
	}
	return mediaTypes[utils.R.IntN(len(mediaTypes))]
}

func (p *RandomProvider) FillString(stringEnum []string, stringMinLength uint64, stringMaxLength *uint64) string {
	if len(stringEnum) > 0 {
		return stringEnum[utils.R.IntN(len(stringEnum))]
	}
	minLen := intOrDefault(stringMinLength, 0)
	maxLen := intOrDefault(stringMaxLength, 8191)

	if minLen > maxLen {
		logrus.Warnf("minLen %d is greater than maxLen %d, adjusting maxLen to %d", minLen, maxLen, minLen)
		maxLen = minLen
	}

	const skew = 2.0 // Skew towards shorter strings
	u := utils.R.Float64()
	n := int(float64(maxLen-minLen)*utils.Pow(u, skew)) + minLen

	return p.randFixedLengthString(n)
}

func (p *RandomProvider) FillDouble(doubleMin *float64, doubleMax *float64) float64 {
	minDouble := float64OrDefault(doubleMin, -2147483648)
	maxDouble := float64OrDefault(doubleMax, 2147483647)

	if minDouble > maxDouble {
		logrus.Warnf("minDouble %f is greater than maxDouble %f, adjusting maxDouble to %f", minDouble, maxDouble, minDouble)
		maxDouble = minDouble
	}

	// Here, we have a 50/50 chance to generate random value by using minDouble maxDouble constraints
	// or just generate some malicious random float64 value
	if utils.R.IntN(2) == 0 {
		return minDouble + utils.R.Float64()*(maxDouble-minDouble)
	} else {
		float64Array := []float64{
			-1.7976931348623157e+308, // Min negative float64
			-3.4028234663852886e+38,  // Min negative float32
			1.401298464324817e-45,    // Smallest positive float32
			4.9406564584124654e-324,  // Smallest positive float64
			3.4028234663852886e+38,   // Max float32
			1.7976931348623157e+308,  // Max float64
			0,                        // Zero
		}
		return float64Array[utils.R.IntN(len(float64Array))]
	}

}

func (p *RandomProvider) FillInteger(min *float64, max *float64) int64 {
	// convert *float64 to int64
	minInt := int64(float64OrDefault(min, -2147483648))
	maxInt := int64(float64OrDefault(max, 2147483647))

	if minInt > maxInt {
		logrus.Warnf("minInt %d is greater than maxInt %d, adjusting maxInt to %d", minInt, maxInt, minInt)
		maxInt = minInt
	}

	// Here, we have a 50/50 chance to generate random value by using minInt maxInt constraints
	// or just generate some malicious random int64 value
	if utils.R.IntN(2) == 0 {
		return minInt + utils.R.Int64N(maxInt-minInt+1)
	} else {
		int64Array := []int64{
			-9223372036854775808, // Min int64
			-2147483648,          // Min int32
			-32768,               // Min int16
			-128,                 // Min int8
			0,                    // Zero
			127,                  // Max int8
			32767,                // Max int16
			2147483647,           // Max int32
			9223372036854775807,  // Max int64
		}
		return int64Array[utils.R.IntN(len(int64Array))]
	}
}

func (p *RandomProvider) FillBoolean() bool {
	// Randomly return true or false
	return utils.R.IntN(2) == 0
}

// randFixedLengthString generates a random alphanumeric string of length n.
func (p *RandomProvider) randFixedLengthString(n int) string {
	letters := []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~!$'()*+,;:@/?")

	b := make([]rune, n)
	for i := range b {
		b[i] = letters[utils.R.IntN(len(letters))]
	}
	return string(b)
}
