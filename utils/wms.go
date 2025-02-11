package utils

// #include "gdal.h"
//#include "gdal_alg.h"
//#cgo pkg-config: gdal
import "C"

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"text/template"
	"time"
	"unsafe"

	geo "github.com/nci/geometry"
	"github.com/nci/gomemcache/memcache"
)

const ISOZeroTime = "0001-01-01T00:00:00.000Z"
const WeightedTimeAxis = "weighted_time"

// supported formats. Second one added for third party apps compatibility e.g QGIS
var ISOTimeFormats = []string{ISOFormat, "2006-01-02T15:04:05Z"}

type AxisIdxSelector struct {
	Start   *int
	End     *int
	Step    *int
	IsRange bool
	IsAll   bool
}

type AxisParam struct {
	Name         string    `json:"name"`
	Start        *float64  `json:"start,omitempty"`
	End          *float64  `json:"end,omitempty"`
	InValues     []float64 `json:"in_values,omitempty"`
	Order        int       `json:"order,omitempty"`
	Aggregate    int       `json:"aggregate,omitempty"`
	IdxSelectors []*AxisIdxSelector
}

// WMSParams contains the serialised version
// of the parameters contained in a WMS request.
type WMSParams struct {
	Service          *string      `json:"service,omitempty"`
	Request          *string      `json:"request,omitempty"`
	CRS              *string      `json:"crs,omitempty"`
	BBox             []float64    `json:"bbox,omitempty"`
	Format           *string      `json:"format,omitempty"`
	X                *int         `json:"x,omitempty"`
	Y                *int         `json:"y,omitempty"`
	Height           *int         `json:"height,omitempty"`
	Width            *int         `json:"width,omitempty"`
	Time             *time.Time   `json:"time,omitempty"`
	Layers           []string     `json:"layers,omitempty"`
	Styles           []string     `json:"styles,omitempty"`
	Version          *string      `json:"version,omitempty"`
	Axes             []*AxisParam `json:"axes,omitempty"`
	Offset           *float64     `json:"offset,omitempty"`
	Clip             *float64     `json:"clip,omitempty"`
	Palette          *string      `json:"palette,omitempty"`
	ColourScale      *int         `json:"colour_scale,omitempty"`
	BandExpr         *BandExpressions
	GeojsonFeatureId *string `json:"geojson_feature_id,omitempty"`
}

// WMSRegexpMap maps WMS request parameters to
// regular expressions for doing validation
// when parsing.
// --- These regexp do not avoid every case of
// --- invalid code but filter most of the malformed
// --- cases. Error free JSON deserialisation into types
// --- also validates correct values.
var WMSRegexpMap = map[string]string{"service": `^WMS$`,
	"request": `^GetCapabilities$|^GetFeatureInfo$|^DescribeLayer$|^GetMap$|^GetLegendGraphic$`,
	"crs":     `^(?i)(?:[A-Z]+):(?:[0-9]+)$`,
	"bbox":    `^[-+]?[0-9]*\.?[0-9]*([eE][-+]?[0-9]+)?(,[-+]?[0-9]*\.?[0-9]*([eE][-+]?[0-9]+)?){3}$`,
	"x":       `^[0-9]+$`,
	"y":       `^[0-9]+$`,
	"width":   `^[0-9]+$`,
	"height":  `^[0-9]+$`,
	"axis":    `^[A-Za-z_][A-Za-z0-9_]*$`,
	"time":    `^\d{4}-(?:1[0-2]|0[1-9])-(?:3[01]|0[1-9]|[12][0-9])T[0-2]\d:[0-5]\d:[0-5]\d(Z|\.\d+Z)$`}

// BBox2Geot return the geotransform from the
// parameters received in a WMS GetMap request
func BBox2Geot(width, height int, bbox []float64) []float64 {
	return []float64{bbox[0], (bbox[2] - bbox[0]) / float64(width), 0, bbox[3], 0, (bbox[1] - bbox[3]) / float64(height)}
}

func CompileWMSRegexMap() map[string]*regexp.Regexp {
	REMap := make(map[string]*regexp.Regexp)
	for key, re := range WMSRegexpMap {
		REMap[key] = regexp.MustCompile(re)
	}

	return REMap
}

func CheckWMSVersion(version string) bool {
	return version == "1.3.0" || version == "1.1.1"
}

// GetGeojsonFeature returns a geo.Feature from the passed geojson id.
// This is requested from the configured WmsGeojsonClipConfig.GeojsonGetEndpoint
func GetGeojsonFeature(geojsonFeatureId string, wmsClipConfig WmsGeojsonClipConfig, mc *memcache.Client) (*geo.Feature, error) {

	if wmsClipConfig.GeojsonGetEndpoint == "" {
		return nil, fmt.Errorf("geojson endpoint not configured")
	}

	geojsonUrl := fmt.Sprintf("%s/%s", wmsClipConfig.GeojsonGetEndpoint, geojsonFeatureId)

	r, err := http.Get(geojsonUrl)

	if err != nil {
		return nil, fmt.Errorf("error getting geojson with id: %s using url: %s", geojsonFeatureId, geojsonUrl)
	}

	defer r.Body.Close()

	b, err := io.ReadAll(r.Body)

	if err != nil {
		return nil, fmt.Errorf("error reading geojson response")
	}

	featColl := &geo.FeatureCollection{}

	err = json.Unmarshal(b, featColl)

	if err != nil {
		return nil, fmt.Errorf("error unmarshalling geojson response")
	}

	// return first feature. Only one feature is expected bacl
	if len(featColl.Features) > 0 {
		return &featColl.Features[0], nil
	}

	return nil, fmt.Errorf("no feature found")
}

// WMSParamsChecker checks and marshals the content
// of the parameters of a WMS request into a
// WMSParams struct.
func WMSParamsChecker(params map[string][]string, compREMap map[string]*regexp.Regexp) (WMSParams, error) {

	var wmsParams WMSParams

	jsonFields := []string{}

	if service, serviceOK := params["service"]; serviceOK {
		if compREMap["service"].MatchString(service[0]) {
			jsonFields = append(jsonFields, fmt.Sprintf(`"service":"%s"`, service[0]))
		}
	}

	if version, versionOK := params["version"]; versionOK {
		jsonFields = append(jsonFields, fmt.Sprintf(`"version":"%s"`, version[0]))
	}

	if request, requestOK := params["request"]; requestOK {
		jsonFields = append(jsonFields, fmt.Sprintf(`"request":"%s"`, request[0]))
	}

	// WMS specifies that coordinate reference systems can be designed by either: ["srs", "crs"]
	if value, srsOK := params["srs"]; srsOK {
		params["crs"] = value
		delete(params, "srs")
	}

	if crs, crsOK := params["crs"]; crsOK {
		if compREMap["crs"].MatchString(crs[0]) {
			jsonFields = append(jsonFields, fmt.Sprintf(`"crs":"%s"`, crs[0]))
		}
	}

	if bbox, bboxOK := params["bbox"]; bboxOK {
		if compREMap["bbox"].MatchString(bbox[0]) {
			jsonFields = append(jsonFields, fmt.Sprintf(`"bbox":[%s]`, bbox[0]))
		}
	}

	if i, iOK := params["i"]; iOK {
		params["x"] = i
	}

	if x, xOK := params["x"]; xOK {
		if compREMap["x"].MatchString(x[0]) {
			jsonFields = append(jsonFields, fmt.Sprintf(`"x":%s`, x[0]))
		}
	}

	if j, jOK := params["j"]; jOK {
		params["y"] = j
	}

	if y, yOK := params["y"]; yOK {
		if compREMap["y"].MatchString(y[0]) {
			jsonFields = append(jsonFields, fmt.Sprintf(`"y":%s`, y[0]))
		}
	}

	if width, widthOK := params["width"]; widthOK {
		if compREMap["width"].MatchString(width[0]) {
			jsonFields = append(jsonFields, fmt.Sprintf(`"width":%s`, width[0]))
		}
	}

	if height, heightOK := params["height"]; heightOK {
		if compREMap["height"].MatchString(height[0]) {
			jsonFields = append(jsonFields, fmt.Sprintf(`"height":%s`, height[0]))
		}
	}

	if geojsonFeatureId, geojsonFeatureIdOk := params["geojson_feature_id"]; geojsonFeatureIdOk && geojsonFeatureId[0] != "" {
		jsonFields = append(jsonFields, fmt.Sprintf(`"geojson_feature_id":"%s"`, geojsonFeatureId[0]))
	}

	if timeRaw, timeOK := params["time"]; timeOK {
		var times []string
		for _, t := range strings.Split(timeRaw[0], ",") {
			t = strings.TrimSpace(t)
			if len(t) == 0 {
				continue
			}

			if compREMap["time"].MatchString(t) {
				times = append(times, t)
			}
		}

		if len(times) == 0 {
			return wmsParams, fmt.Errorf("invalid time format")
		} else {
			jsonFields = append(jsonFields, fmt.Sprintf(`"time":"%s"`, times[0]))
			if len(times) > 1 {
				axis := &AxisParam{Name: WeightedTimeAxis, Aggregate: 0}
				for _, tStr := range times {
					t, err := parseTime(tStr)
					if err != nil {
						return wmsParams, fmt.Errorf("invalid time format")
					}
					val := float64(t.Unix())
					axis.InValues = append(axis.InValues, val)
				}
				wmsParams.Axes = append(wmsParams.Axes, axis)
			}
		}
	}

	var layers []string
	if _layers, layersOK := params["layers"]; layersOK {
		layers = _layers
	} else {
		if _layer, layerOK := params["layer"]; layerOK {
			layers = _layer
		}
	}
	if len(layers) > 0 {
		if !strings.Contains(layers[0], "\"") {
			jsonFields = append(jsonFields, fmt.Sprintf(`"layers":["%s"]`, strings.Replace(layers[0], ",", "\",\"", -1)))
		}
	}

	if styles, stylesOK := params["styles"]; stylesOK {
		if !strings.Contains(styles[0], "\"") {
			jsonFields = append(jsonFields, fmt.Sprintf(`"styles":["%s"]`, strings.Replace(styles[0], ",", "\",\"", -1)))
		}
	}

	axesInfo := []string{}

	for key, val := range params {
		if strings.HasPrefix(key, "dim_") {
			if len(key) <= len("dim_") {
				continue
			}

			axisName := key[len("dim_"):]
			axisName = strings.TrimSpace(axisName)

			if axisName == "palette" {
				params["palette"] = val
				continue
			}

			if axisName == "colorscale" {
				params["colorscale"] = val
				continue
			}

			if !compREMap["axis"].MatchString(axisName) {
				return wmsParams, fmt.Errorf("invalid axis name: %v", key)
			}

			valFloat64, err := strconv.ParseFloat(val[0], 64)
			if err != nil {
				continue
			}

			axisVal := valFloat64

			axesInfo = append(axesInfo, fmt.Sprintf(`{"name":"%s", "start":%f, "order":1, "aggregate": 1}`, axisName, axisVal))
		}
	}

	if colourScale, colourScaleOK := params["colorscale"]; colourScaleOK {
		colourScaleStr := strings.ToLower(strings.TrimSpace(colourScale[0]))
		colourScale := -1
		if colourScaleStr == "linear" {
			colourScale = ColourLinearScale
		} else if colourScaleStr == "logarithm" {
			colourScale = ColourLogScale
		}
		if colourScale >= 0 {
			jsonFields = append(jsonFields, fmt.Sprintf(`"colour_scale": %d`, colourScale))
		}
	}

	if colourScheme, colourSchemeOK := params["colorscheme"]; colourSchemeOK {
		params["palette"] = colourScheme
	}

	if palette, paletteOK := params["palette"]; paletteOK {
		jsonFields = append(jsonFields, fmt.Sprintf(`"palette": "%s"`, palette[0]))
	}

	if scaleRange, scaleRangeOK := params["colorscalerange"]; scaleRangeOK {
		parts := strings.Split(scaleRange[0], ",")
		if len(parts) == 2 {
			lower, err := strconv.ParseFloat(parts[0], 64)
			if err != nil {
				return wmsParams, fmt.Errorf("parsing error in the lower endpoint of colorscalerange: %v", err)
			}
			offset := 0.0 - lower
			jsonFields = append(jsonFields, fmt.Sprintf(`"offset": %f`, offset))

			upper, err := strconv.ParseFloat(parts[1], 64)
			if err != nil {
				return wmsParams, fmt.Errorf("parsing error in the upper endpoint of colorscalerange: %v", err)
			}
			if lower > upper {
				return wmsParams, fmt.Errorf("lower > upper, colorscalerange must be [min,max]: %v", scaleRange[0])
			}
			clip := upper - lower
			jsonFields = append(jsonFields, fmt.Sprintf(`"clip": %f`, clip))
		} else {
			return wmsParams, fmt.Errorf("colorscalerange must be in the format of 'min,max': %v", scaleRange[0])
		}
	} else {
		if _, codeOK := params["code"]; codeOK {
			jsonFields = append(jsonFields, fmt.Sprintf(`"offset": %f`, 0.0))
			jsonFields = append(jsonFields, fmt.Sprintf(`"clip": %f`, 1.0))
		}
	}

	jsonFields = append(jsonFields, fmt.Sprintf(`"axes":[%s]`, strings.Join(axesInfo, ",")))
	jsonParams := fmt.Sprintf("{%s}", strings.Join(jsonFields, ","))

	axesTmp := wmsParams.Axes
	err := json.Unmarshal([]byte(jsonParams), &wmsParams)
	if err != nil {
		return wmsParams, err
	}
	for _, axis := range axesTmp {
		if axis != nil {
			wmsParams.Axes = append(wmsParams.Axes, axis)
		}
	}

	if subsets, subsetsOK := params["subset"]; subsetsOK {
		sub := strings.Join(subsets, ";")
		axes, err := parseSubsetClause(sub, compREMap)
		if err != nil {
			return wmsParams, err
		}

		for _, axis := range axes {
			wmsParams.Axes = append(wmsParams.Axes, axis)
		}
	}

	foundTime := false
	for _, axis := range wmsParams.Axes {
		if axis.Name == "time" {
			foundTime = true
		}
	}

	if !foundTime {
		wmsParams.Axes = append(wmsParams.Axes, &AxisParam{Name: "time", Aggregate: 1})
	}

	codeFormats, codeFormatOK := params["code_format"]
	var codeFormat string
	if codeFormatOK {
		codeFormat = strings.ToLower(strings.TrimSpace(codeFormats[0]))
		if codeFormat != "plain" && codeFormat != "base64" {
			return wmsParams, fmt.Errorf("code_format must be either plain or base64")
		}
	}

	if code, codeOK := params["code"]; codeOK {
		params["rangesubset"] = code
	}

	if rangeSubsets, rangeSubsetsOK := params["rangesubset"]; rangeSubsetsOK {
		if codeFormatOK && codeFormat == "base64" {
			for ir, s := range rangeSubsets {
				data, err := base64.StdEncoding.DecodeString(s)
				if err != nil {
					return wmsParams, err
				}
				rangeSubsets[ir] = string(data)
			}
		}

		sub := strings.Join(rangeSubsets, ";")
		parts := strings.Split(sub, ";")

		var rangeSubs []string
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if len(p) < 1 {
				continue
			}

			rangeSubs = append(rangeSubs, p)
		}

		bandExpr, err := ParseBandExpressions(rangeSubs)
		if err != nil {
			return wmsParams, fmt.Errorf("parsing error in band expressions: %v", err)
		}

		wmsParams.BandExpr = bandExpr
	}

	return wmsParams, err
}

// GetCoordinates returns the x and y
// coordinates in the original projection
// from the tile relative WMS parameters.
func GetCoordinates(params WMSParams) (float64, float64, error) {
	if len(params.BBox) != 4 {
		return 0, 0, fmt.Errorf("No BBox parameter has been specified")
	}
	if params.Width == nil || params.Height == nil {
		return 0, 0, fmt.Errorf("Width and Height have to be bigger than 0")
	}

	return params.BBox[0] + (params.BBox[2]-params.BBox[0])*float64(*params.X)/float64(*params.Width), params.BBox[3] + (params.BBox[1]-params.BBox[3])*float64(*params.Y)/float64(*params.Height), nil
}

// GetLayerIndex returns the index of the
// specified layer inside the Config.Layers
// field.
func GetLayerIndex(params WMSParams, config *Config) (int, error) {
	if params.Layers != nil {
		product := params.Layers[0]
		for i := range config.Layers {
			if config.Layers[i].Name == product {
				return i, nil
			}
		}
		return -1, fmt.Errorf("%s not found in config Layers", product)
	}
	return -1, fmt.Errorf("WMS request doesn't specify a product")
}

// GetLayerStyleIndex returns the index of the
// specified style inside a layer
func GetLayerStyleIndex(params WMSParams, config *Config, layerIdx int) (int, error) {
	if params.Styles != nil {
		style := strings.TrimSpace(params.Styles[0])
		if !strings.HasPrefix(style, "__tw__") {
			for _, axis := range params.Axes {
				if axis.Name == WeightedTimeAxis {
					style = "__tw__" + style
					break
				}
			}
		}

		if len(style) == 0 {
			if len(config.Layers[layerIdx].Styles) > 0 {
				return 0, nil
			} else {
				return -1, nil
			}
		}
		for i := range config.Layers[layerIdx].Styles {
			if config.Layers[layerIdx].Styles[i].Name == style {
				return i, nil
			}
		}
		return -1, fmt.Errorf("style %s not found in this layer", style)
	} else {
		if len(config.Layers[layerIdx].Styles) > 0 {
			return 0, nil
		}
	}
	return -1, nil
}

func ExecuteWriteTemplateFile(w io.Writer, data interface{}, filePath string) error {
	// General template compilation, execution and writting in to
	// a stream.
	tplStr, err := ioutil.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("Error trying to read %s file: %v", filePath, err)
	}
	tpl, err := template.New("template").Parse(string(tplStr))
	if err != nil {
		return fmt.Errorf("Error trying to parse template document: %v", err)
	}
	err = tpl.Execute(w, data)
	if err != nil {
		return fmt.Errorf("Error executing template: %v\n", err)
	}

	return nil
}

// GetCurrentTimeStamp gets the current timestamp if time is not
// specified in the HTTP request
func GetCurrentTimeStamp(timestamps []string) (*time.Time, error) {
	var currentTime time.Time

	// Empty timestamps often indicate something wrong with user data, GSKY config files,
	// or both. We simply fill Now() to prevent the out-of-range index error for the Dates
	// array. The implification of this is that users will get a blank image in the HTTP
	// response instead of the 500 internal server error.
	if len(timestamps) == 0 {
		currentTime = time.Now().UTC()
	} else {
		tmpTime, err := time.Parse(ISOFormat, timestamps[len(timestamps)-1])
		if err != nil {
			return nil, fmt.Errorf("Cannot find a valid date to proceed with the request")
		}
		currentTime = tmpTime
	}

	return &currentTime, nil
}

func CheckDisableServices(layer *Layer, service string) bool {
	if len(layer.DisableServices) > 0 {
		if layer.DisableServicesMap == nil {
			layer.DisableServicesMap = make(map[string]struct{})
			for _, srv := range layer.DisableServices {
				srv = strings.ToLower(strings.TrimSpace(srv))
				layer.DisableServicesMap[srv] = struct{}{}
			}
		}

		if _, found := layer.DisableServicesMap[service]; found {
			return true
		}
	}

	return false
}

func GetCanonicalBbox(srs string, bbox []float64) ([]float64, error) {
	srs = strings.ToUpper(strings.TrimSpace(srs))
	dst := "EPSG:3857"
	if srs == dst {
		box := make([]float64, len(bbox))
		for i := 0; i < len(bbox); i++ {
			box[i] = bbox[i]
		}
		return box, nil
	}

	var opts []*C.char
	opts = append(opts, C.CString(fmt.Sprintf("SRC_SRS=%s", srs)))
	opts = append(opts, C.CString(fmt.Sprintf("DST_SRS=%s", dst)))
	for _, opt := range opts {
		defer C.free(unsafe.Pointer(opt))
	}
	opts = append(opts, nil)
	transformArg := C.GDALCreateGenImgProjTransformer2(nil, nil, &opts[0])
	if transformArg == nil {
		return bbox, fmt.Errorf("GDALCreateGenImgProjTransformer2 failed")
	}
	defer C.GDALDestroyGenImgProjTransformer(transformArg)

	dx := []C.double{C.double(bbox[0]), C.double(bbox[2])}
	dy := []C.double{C.double(bbox[1]), C.double(bbox[3])}
	dz := make([]C.double, 2)
	bSuccess := make([]C.int, 2)

	C.GDALGenImgProjTransform(transformArg, C.int(0), 2, &dx[0], &dy[0], &dz[0], &bSuccess[0])
	if bSuccess[0] != 0 && bSuccess[1] != 0 {
		return []float64{float64(dx[0]), float64(dy[0]), float64(dx[1]), float64(dy[1])}, nil
	} else {
		return bbox, fmt.Errorf("GDALGenImgProjTransform failed")
	}
}

func GetPixelResolution(bbox []float64, width int, height int) float64 {
	xRes := (bbox[2] - bbox[0]) / float64(width)
	yRes := (bbox[3] - bbox[1]) / float64(height)
	reqRes := xRes
	if yRes > reqRes {
		reqRes = yRes
	}
	return reqRes
}

func FindLayerBestOverview(layer *Layer, reqRes float64, allowExtrapolation bool) int {
	bestOvr := -1
	if reqRes > layer.ZoomLimit {
		if !allowExtrapolation {
			if layer.Overviews[0].ZoomLimit > reqRes {
				return -1
			}
		}

		iOvr := 0
		for i := 0; i < len(layer.Overviews); i++ {
			if layer.Overviews[i].ZoomLimit > layer.ZoomLimit {
				break
			}
			iOvr = i
		}
		bestOvr = iOvr
	}
	return bestOvr
}

func parseTime(input string) (time.Time, error) {
	for _, format := range ISOTimeFormats {
		t, err := time.Parse(format, input)
		if err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognized time format")
}
