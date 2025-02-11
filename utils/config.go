package utils

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/color"
	"io"
	"io/ioutil"
	"log"
	"math"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	goeval "github.com/edisonguo/govaluate"
	"github.com/edisonguo/jet"
	pb "github.com/nci/gsky/worker/gdalservice"
	geojson "github.com/paulmach/go.geojson"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
)

var EtcDir = "."
var DataDir = "."
var GSKYVersion = "."

const ReservedMemorySize = 1.5 * 1024 * 1024 * 1024
const ColourLinearScale = 0
const ColourLogScale = 1

const DefaultRecvMsgSize = 10 * 1024 * 1024

const DefaultWmsPolygonSegments = 2
const DefaultWcsPolygonSegments = 10

const DefaultWmsTimeout = 20
const DefaultWcsTimeout = 30
const DefaultWpsTimeout = 300

const DefaultGrpcWmsConcPerNode = 16
const DefaultGrpcWcsConcPerNode = 16
const DefaultGrpcWpsConcPerNode = 16

const DefaultWmsPolygonShardConcLimit = 2
const DefaultWcsPolygonShardConcLimit = 2

const DefaultWmsMaxWidth = 512
const DefaultWmsMaxHeight = 512
const DefaultWcsMaxWidth = 50000
const DefaultWcsMaxHeight = 30000
const DefaultWcsMaxTileWidth = 1024
const DefaultWcsMaxTileHeight = 1024

const DefaultLegendWidth = 160
const DefaultLegendHeight = 320

const DefaultConcGrpcWorkerQuery = 64

const DefaultWmsMaxBandVariables = 6
const DefaultWmsMaxBandTokens = 75
const DefaultWmsMaxBandExpressions = 3

const DefaultWcsMaxBandVariables = 10
const DefaultWcsMaxBandTokens = 300
const DefaultWcsMaxBandExpressions = 10

type ServiceConfig struct {
	OWSHostname       string `json:"ows_hostname"`
	OWSProtocol       string `json:"ows_protocol"`
	NameSpace         string
	MASAddress        string   `json:"mas_address"`
	WorkerNodes       []string `json:"worker_nodes"`
	OWSClusterNodes   []string `json:"ows_cluster_nodes"`
	TempDir           string   `json:"temp_dir"`
	MaxGrpcBufferSize int      `json:"max_grpc_buffer_size"`
	EnableAutoLayers  bool     `json:"enable_auto_layers"`
	OWSCacheGPath     string   `json:"ows_cache_gpath"`
}

type Mask struct {
	ID            string   `json:"id"`
	Value         string   `json:"value"`
	DataSource    string   `json:"data_source"`
	Inclusive     bool     `json:"inclusive"`
	BitTests      []string `json:"bit_tests"`
	IDExpressions *BandExpressions
}

type Palette struct {
	Name        string       `json:"name"`
	Interpolate bool         `json:"interpolate"`
	Colours     []color.RGBA `json:"colours"`
}

type BandExpressionComplexityCriteria struct {
	MaxVariables   int                    `json:"max_variables"`
	MaxTokens      int                    `json:"max_tokens"`
	MaxExpressions int                    `json:"max_expressions"`
	TokenACL       map[string]interface{} `json:"token_acl"`
	VariableLookup map[string]struct{}
}

type BandExpressions struct {
	ExprText    []string
	Expressions []*goeval.EvaluableExpression
	VarList     []string
	ExprNames   []string
	ExprVarRef  [][]string
}

type LayerAxis struct {
	Name    string   `json:"name"`
	Default string   `json:"default"`
	Values  []string `json:"values"`
}

// Layer contains all the details that a layer needs
// to be published and rendered
type Layer struct {
	OWSHostname                  string   `json:"ows_hostname"`
	OWSProtocol                  string   `json:"ows_protocol"`
	MASAddress                   string   `json:"mas_address"`
	NameSpace                    string   `json:"namespace"`
	Name                         string   `json:"name"`
	Title                        string   `json:"title"`
	Abstract                     string   `json:"abstract"`
	MetadataURL                  string   `json:"metadata_url"`
	VRTURL                       string   `json:"vrt_url"`
	DataURL                      string   `json:"data_url"`
	Overviews                    []Layer  `json:"overviews"`
	InputLayers                  []Layer  `json:"input_layers"`
	DisableServices              []string `json:"disable_services"`
	DisableServicesMap           map[string]struct{}
	DataSource                   string `json:"data_source"`
	StartISODate                 string `json:"start_isodate"`
	EndISODate                   string `json:"end_isodate"`
	EffectiveStartDate           string
	EffectiveEndDate             string
	TimestampToken               string
	StepDays                     int      `json:"step_days"`
	StepHours                    int      `json:"step_hours"`
	StepMinutes                  int      `json:"step_minutes"`
	Accum                        bool     `json:"accum"`
	TimeGen                      string   `json:"time_generator"`
	Dates                        []string `json:"dates"`
	RGBProducts                  []string `json:"rgb_products"`
	RGBExpressions               *BandExpressions
	Mask                         *Mask      `json:"mask"`
	OffsetValue                  float64    `json:"offset_value"`
	ClipValue                    float64    `json:"clip_value"`
	ScaleValue                   float64    `json:"scale_value"`
	Palette                      *Palette   `json:"palette"`
	Palettes                     []*Palette `json:"palettes"`
	LegendPath                   string     `json:"legend_path"`
	LegendHeight                 int        `json:"legend_height"`
	LegendWidth                  int        `json:"legend_width"`
	Styles                       []Layer    `json:"styles"`
	ZoomLimit                    float64    `json:"zoom_limit"`
	MaxGrpcRecvMsgSize           int        `json:"max_grpc_recv_msg_size"`
	WmsPolygonSegments           int        `json:"wms_polygon_segments"`
	WcsPolygonSegments           int        `json:"wcs_polygon_segments"`
	WmsTimeout                   int        `json:"wms_timeout"`
	WcsTimeout                   int        `json:"wcs_timeout"`
	GrpcWmsConcPerNode           int        `json:"grpc_wms_conc_per_node"`
	GrpcWcsConcPerNode           int        `json:"grpc_wcs_conc_per_node"`
	GrpcWpsConcPerNode           int        `json:"grpc_wps_conc_per_node"`
	WmsPolygonShardConcLimit     int        `json:"wms_polygon_shard_conc_limit"`
	WcsPolygonShardConcLimit     int        `json:"wcs_polygon_shard_conc_limit"`
	BandStrides                  int        `json:"band_strides"`
	WmsMaxWidth                  int        `json:"wms_max_width"`
	WmsMaxHeight                 int        `json:"wms_max_height"`
	WcsMaxWidth                  int        `json:"wcs_max_width"`
	WcsMaxHeight                 int        `json:"wcs_max_height"`
	WcsMaxTileWidth              int        `json:"wcs_max_tile_width"`
	WcsMaxTileHeight             int        `json:"wcs_max_tile_height"`
	FeatureInfoMaxAvailableDates int        `json:"feature_info_max_dates"`
	FeatureInfoMaxDataLinks      int        `json:"feature_info_max_data_links"`
	FeatureInfoDataLinkUrl       string     `json:"feature_info_data_link_url"`
	FeatureInfoBands             []string   `json:"feature_info_bands"`
	FeatureInfoExpressions       *BandExpressions
	NoDataLegendPath             string                            `json:"nodata_legend_path"`
	AxesInfo                     []*LayerAxis                      `json:"axes"`
	UserSrcSRS                   int                               `json:"src_srs"`
	UserSrcGeoTransform          int                               `json:"src_geo_transform"`
	DefaultGeoBbox               []float64                         `json:"default_geo_bbox"`
	DefaultGeoSize               []int                             `json:"default_geo_size"`
	WmsAxisMapping               int                               `json:"wms_axis_mapping"`
	GrpcTileXSize                float64                           `json:"grpc_tile_x_size"`
	GrpcTileYSize                float64                           `json:"grpc_tile_y_size"`
	IndexTileXSize               float64                           `json:"index_tile_x_size"`
	IndexTileYSize               float64                           `json:"index_tile_y_size"`
	SpatialExtent                []float64                         `json:"spatial_extent"`
	IndexResLimit                float64                           `json:"index_res_limit"`
	ColourScale                  int                               `json:"colour_scale"`
	TimestampsLoadStrategy       string                            `json:"timestamps_load_strategy"`
	MasQueryHint                 string                            `json:"mas_query_hint"`
	SRSCf                        int                               `json:"srs_cf"`
	Visibility                   string                            `json:"visibility"`
	RasterXSize                  float64                           `json:"raster_x_size"`
	RasterYSize                  float64                           `json:"raster_y_size"`
	WmsBandExpressionCriteria    *BandExpressionComplexityCriteria `json:"wms_band_expr_criteria"`
	WcsBandExpressionCriteria    *BandExpressionComplexityCriteria `json:"wcs_band_expr_criteria"`
}

// Process contains all the details that a WPS needs
// to be published and processed
type Process struct {
	DataSources    []Layer    `json:"data_sources"`
	Identifier     string     `json:"identifier"`
	Title          string     `json:"title"`
	Abstract       string     `json:"abstract"`
	MaxArea        float64    `json:"max_area"`
	LiteralData    []LitData  `json:"literal_data"`
	ComplexData    []CompData `json:"complex_data"`
	IdentityTol    float64    `json:"identity_tol"`
	DpTol          float64    `json:"dp_tol"`
	Approx         *bool      `json:"approx,omitempty"`
	DrillAlgorithm string     `json:"drill_algo,omitempty"`
	PixelStat      string     `json:"pixel_stat,omitempty"`
	WpsTimeout     int        `json:"wps_timeout"`
}

// LitData contains the description of a variable used to compute a
// WPS operation
type LitData struct {
	Identifier    string   `json:"identifier"`
	Title         string   `json:"title"`
	Abstract      string   `json:"abstract"`
	DataType      string   `json:"data_type"`
	DataTypeRef   string   `json:"data_type_ref"`
	AllowedValues []string `json:"allowed_values"`
	MinOccurs     int      `json:"min_occurs"`
}

// CompData contains the description of a variable used to compute a
// WPS operation
type CompData struct {
	Identifier string `json:"identifier"`
	Title      string `json:"title"`
	Abstract   string `json:"abstract"`
	MimeType   string `json:"mime_type"`
	Encoding   string `json:"encoding"`
	Schema     string `json:"schema"`
	MinOccurs  int    `json:"min_occurs"`
}

type CapabilityExtensionProperty struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type CapabilityExtension struct {
	Name        string                        `json:"name"`
	Version     string                        `json:"version"`
	Layer       Layer                         `json:"layer"`
	ResourceURL string                        `json:"resource_url"`
	Properties  []CapabilityExtensionProperty `json:"properties"`
}
type WmsGeojsonClipConfig struct {
	GeojsonGetEndpoint string `json:"geojson_get_endpoint"`
}

// Config is the struct representing the configuration
// of a WMS server. It contains information about the
// file index API as well as the list of WMS layers that
// can be served.
type Config struct {
	ServiceConfig        ServiceConfig         `json:"service_config"`
	Layers               []Layer               `json:"layers"`
	Processes            []Process             `json:"processes"`
	Extensions           []CapabilityExtension `json:"extensions"`
	WmsGeojsonClipConfig WmsGeojsonClipConfig  `json:"wms_geojson_clip_config"`
}

// ISOFormat is the string used to format Go ISO times
const ISOFormat = "2006-01-02T15:04:05.000Z"

func GenerateDatesAux(start, end time.Time, stepMins time.Duration) []string {
	dates := []string{}
	for !start.After(end) {
		dates = append(dates, start.Format(ISOFormat))
		start = time.Date(start.Year()+1, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	return dates
}

// GenerateDatesMCD43A4 function is used to generate the list of ISO
// dates from its especification in the Config.Layer struct.
func GenerateDatesMCD43A4(start, end time.Time, stepMins time.Duration) []string {
	dates := []string{}
	if int64(stepMins) <= 0 {
		return dates
	}
	year := start.Year()
	for !start.After(end) {
		for start.Year() == year && !start.After(end) {
			dates = append(dates, start.Format(ISOFormat))
			start = start.Add(stepMins)
		}
		if start.After(end) {
			break
		}
		year = start.Year()
		start = time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	return dates
}

func GenerateDatesGeoglam(start, end time.Time, stepMins time.Duration) []string {
	dates := []string{}
	if int64(stepMins) <= 0 {
		return dates
	}
	year := start.Year()
	for !start.After(end) {
		for start.Year() == year && !start.After(end) {
			dates = append(dates, start.Format(ISOFormat))
			nextDate := start.AddDate(0, 0, 4)
			if start.Month() == nextDate.Month() {
				start = start.Add(stepMins)
			} else {
				start = nextDate
			}

		}
		if start.After(end) {
			break
		}
		year = start.Year()
		start = time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	return dates
}

func GenerateDatesChirps20(start, end time.Time, stepMins time.Duration) []string {
	dates := []string{}
	for !start.After(end) {
		dates = append(dates, time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, time.UTC).Format(ISOFormat))
		dates = append(dates, time.Date(start.Year(), start.Month(), 11, 0, 0, 0, 0, time.UTC).Format(ISOFormat))
		dates = append(dates, time.Date(start.Year(), start.Month(), 21, 0, 0, 0, 0, time.UTC).Format(ISOFormat))
		start = start.AddDate(0, 1, 0)
	}
	return dates
}

func GenerateMonthlyDates(start, end time.Time, stepMins time.Duration) []string {
	dates := []string{}
	for !start.After(end) {
		dates = append(dates, time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC).Format(ISOFormat))
		start = start.AddDate(0, 1, 0)
	}
	return dates
}

func GenerateYearlyDates(start, end time.Time, stepMins time.Duration) []string {
	dates := []string{}
	for !start.After(end) {
		dates = append(dates, time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC).Format(ISOFormat))
		start = start.AddDate(1, 0, 0)
	}
	return dates
}

func GenerateDatesRegular(start, end time.Time, stepMins time.Duration) []string {
	dates := []string{}
	if int64(stepMins) <= 0 {
		return dates
	}
	for !start.After(end) {
		dates = append(dates, start.Format(ISOFormat))
		start = start.Add(stepMins)
	}
	return dates
}

func GenerateDatesMas(start, end string, masAddress string, collection string, namespaces []string, stepMins time.Duration, token string, verbose bool) ([]string, string) {
	emptyDates := []string{}

	start = strings.TrimSpace(start)
	if len(start) > 0 {
		_, err := time.Parse(ISOFormat, start)
		if err != nil {
			log.Printf("start date parsing error: %v", err)
			return emptyDates, token
		}
	}

	end = strings.TrimSpace(end)
	if len(end) > 0 {
		_, err := time.Parse(ISOFormat, end)
		if err != nil {
			log.Printf("end date parsing error: %v", err)
			return emptyDates, token
		}
	}

	ns := strings.Join(namespaces, ",")
	url := strings.Replace(fmt.Sprintf("http://%s%s?timestamps&time=%s&until=%s&namespace=%s&token=%s", masAddress, collection, start, end, ns, token), " ", "%20", -1)
	if verbose {
		log.Printf("config querying MAS for timestamps: %v", url)
	}

	resp, err := http.Get(url)
	if err != nil {
		log.Printf("MAS http error: %v,%v", url, err)
		return emptyDates, token
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Printf("MAS http error: %v,%v", url, err)
		return emptyDates, token
	}

	type MasTimestamps struct {
		Error      string   `json:"error"`
		Timestamps []string `json:"timestamps"`
		Token      string   `json:"token"`
	}

	var timestamps MasTimestamps
	err = json.Unmarshal(body, &timestamps)
	if err != nil {
		log.Printf("MAS json response error: %v", err)
		return emptyDates, token
	}

	if len(timestamps.Error) > 0 {
		log.Printf("MAS returned error: %v", timestamps.Error)
		return emptyDates, token
	}

	if timestamps.Token == token {
		return emptyDates, token
	}

	if verbose {
		log.Printf("MAS returned %v timestamps", len(timestamps.Timestamps))
	}

	if int64(stepMins) > 0 && len(timestamps.Timestamps) > 0 {
		startDate, err := time.Parse(ISOFormat, timestamps.Timestamps[0])
		if err != nil {
			log.Printf("Error parsing MAS returned start date: %v", err)
			return emptyDates, token
		}
		endDate, err := time.Parse(ISOFormat, timestamps.Timestamps[len(timestamps.Timestamps)-1])
		if err != nil {
			log.Printf("Error parsing MAS returned end date: %v", err)
			return emptyDates, token
		}

		refDates := []time.Time{}
		for !startDate.After(endDate) {
			refDates = append(refDates, startDate)
			startDate = startDate.Add(stepMins)
		}
		refDates = append(refDates, endDate)

		if len(refDates) > len(timestamps.Timestamps) {
			refDates = refDates[:len(timestamps.Timestamps)]
		}
		aggregatedTimestamps := make([]string, len(refDates))

		iBgn := 0
		for iRef, refTs := range refDates {
			ts0 := time.Time{}
			for it := iBgn; it < len(timestamps.Timestamps); it++ {
				tsStr := timestamps.Timestamps[it]
				ts, err := time.Parse(ISOFormat, tsStr)
				if err != nil {
					log.Printf("Error parsing MAS returned date: %v", err)
					return emptyDates, token
				}

				refDiff := int64(refTs.Sub(ts))
				if refDiff == 0 {
					aggregatedTimestamps[iRef] = tsStr
					iBgn = it + 1
					break
				}

				if refDiff < 0 {
					if it > iBgn {
						refDiff0 := refTs.Sub(ts0)
						if math.Abs(float64(refDiff)) >= math.Abs(float64(refDiff0)) {
							tsStr = timestamps.Timestamps[it-1]
							iBgn = it
						}
					}
					aggregatedTimestamps[iRef] = tsStr
					break
				}

				ts0 = ts
			}
		}

		if verbose {
			log.Printf("Aggregated timestamps: %v, steps: %v", len(aggregatedTimestamps), stepMins)
		}
		return aggregatedTimestamps, timestamps.Token
	}

	return timestamps.Timestamps, timestamps.Token
}

func GenerateDates(name string, start, end time.Time, stepMins time.Duration) []string {
	dateGen := make(map[string]func(time.Time, time.Time, time.Duration) []string)
	dateGen["aux"] = GenerateDatesAux
	dateGen["mcd43"] = GenerateDatesMCD43A4
	dateGen["geoglam"] = GenerateDatesGeoglam
	dateGen["chirps20"] = GenerateDatesChirps20
	dateGen["regular"] = GenerateDatesRegular
	dateGen["monthly"] = GenerateMonthlyDates
	dateGen["yearly"] = GenerateYearlyDates

	if _, ok := dateGen[name]; !ok {
		return []string{}
	}

	return dateGen[name](start, end, stepMins)
}

func symWalk(rootDir string, symlink string, walkFunc filepath.WalkFunc) error {
	return filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		fileName, err := filepath.Rel(rootDir, path)
		if err != nil {
			return err
		}
		path = filepath.Join(symlink, fileName)

		if info.Mode()&os.ModeSymlink == os.ModeSymlink {
			realPath, err := filepath.EvalSymlinks(path)
			if err != nil {
				return err
			}

			info, err := os.Lstat(realPath)
			if err != nil {
				return err
			}

			if info.IsDir() {
				return symWalk(realPath, path, walkFunc)
			}
		}
		return walkFunc(path, info, err)
	})
}

func LoadConfigOnDemand(searchPath string, namespace string, verbose bool) (map[string]*Config, error) {
	searchPathList := strings.Split(searchPath, ":")
	for _, rootDir := range searchPathList {
		rootDir = strings.TrimSpace(rootDir)
		if len(rootDir) == 0 {
			continue
		}
		configFile := filepath.Join(rootDir, namespace, "config.json")
		absPath, err := filepath.Abs(configFile)
		if verbose {
			log.Printf("Loading config on-demand, namespace: %s, configFile: %s, AbsPath: %s", namespace, configFile, absPath)
		}
		if err != nil {
			return nil, err
		}
		if !strings.HasPrefix(absPath, rootDir) {
			return nil, fmt.Errorf("Invalid namespace: %s", namespace)
		}

		if _, err := os.Stat(absPath); err == nil {
			conf, err := LoadAllConfigFiles(absPath, verbose)
			if err != nil {
				return nil, err
			}
			if _, ok := conf["."]; !ok {
				return nil, fmt.Errorf("Error in loading namespace: %s", namespace)
			}

			configMap := make(map[string]*Config)
			err = conf["."].postprocessConfig(namespace)
			if err != nil {
				return nil, fmt.Errorf("Error in on-demand postprocessConfig: %v", err)
			}

			configMap[namespace] = conf["."]
			return configMap, nil
		}
	}
	return nil, fmt.Errorf("namespace not found: %s", namespace)
}

type MASLayers struct {
	Error  string  `json:"error"`
	Layers []Layer `json:"layers"`
}

func LoadLayersFromMAS(masAddress, namespace string, verbose bool) (*MASLayers, error) {
	queryOp := "generate_layers"
	url := strings.Replace(fmt.Sprintf("http://%s/%s?%s", masAddress, namespace, queryOp), " ", "%20", -1)
	if verbose {
		log.Printf("%s: %s", queryOp, url)
	}
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("MAS (%s) error: %v,%v", queryOp, url, err)
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("MAS (%s) error: %v,%v", queryOp, url, err)
	}

	masLayers := &MASLayers{}
	err = json.Unmarshal(body, masLayers)
	if err != nil {
		return nil, fmt.Errorf("MAS (%s) json response error: %v", queryOp, err)
	}

	if len(masLayers.Error) > 0 {
		return nil, fmt.Errorf("MAS (%s) json response error: %v", queryOp, masLayers.Error)
	}

	for _, layer := range masLayers.Layers {
		for ia := range layer.AxesInfo {
			if len(layer.AxesInfo[ia].Values) > 0 {
				layer.AxesInfo[ia].Default = layer.AxesInfo[ia].Values[0]
			}
		}
	}
	return masLayers, nil
}

func LoadLayersFromConfigByDataSource(dataSource string, confMap map[string]*Config, verbose bool) (map[string][]Layer, error) {
	allConfigLayers := map[string][]Layer{}

	for configNamespace, config := range confMap {
		if len(config.Layers) > 0 {
			var configLayers []Layer

			// loop each layer
			for _, layer := range config.Layers {
				layerDataSource := "/" + strings.Trim(layer.DataSource, "/")
				// match datasources
				if layerDataSource == dataSource {
					// skip if matches so as not to duplicate with raw gsky layers
					if strings.Trim(layerDataSource, "/") != configNamespace {
						configLayers = append(configLayers, layer)
					}
				}
			}

			if len(configLayers) > 0 {
				allConfigLayers[configNamespace] = configLayers
			}
		}
	}

	return allConfigLayers, nil
}

func LoadConfigFromMAS(masAddress, namespace string, rootConfig *Config, verbose bool) (map[string]*Config, error) {
	masLayers, err := LoadLayersFromMAS(masAddress, namespace, verbose)
	if err != nil {
		return nil, err
	}

	config := &Config{
		ServiceConfig: ServiceConfig{
			MASAddress:  rootConfig.ServiceConfig.MASAddress,
			WorkerNodes: rootConfig.ServiceConfig.WorkerNodes,
		},
	}

	config.Layers = make([]Layer, len(masLayers.Layers))
	for il, layer := range masLayers.Layers {
		config.Layers[il] = layer
	}

	configStr, cfgErr := json.Marshal(config)
	if cfgErr != nil {
		return nil, fmt.Errorf("MAS config error: %v", cfgErr)
	}

	config = &Config{}
	err = config.LoadConfigString(configStr, verbose)
	if err != nil {
		return nil, fmt.Errorf("MAS config error: %v", err)
	}

	err = config.postprocessConfig(namespace)
	if err != nil {
		return nil, fmt.Errorf("MAS config error: %v", err)
	}

	configMap := make(map[string]*Config)
	configMap[namespace] = config

	for _, conf := range configMap {
		if conf == nil {
			continue
		}
		for i := range conf.Layers {
			err = conf.processFusionTimestamps(i, configMap)
			if err != nil {
				return nil, err
			}
			err = conf.processFusionColourPalette(i, configMap)
			if err != nil {
				return nil, err
			}
		}
	}
	return configMap, nil
}

func LoadAllConfigFiles(searchPath string, verbose bool) (map[string]*Config, error) {
	var err error
	configMap := make(map[string]*Config)
	searchPathList := strings.Split(searchPath, ":")
	for _, rootDir := range searchPathList {
		rootDir = strings.TrimSpace(rootDir)
		if len(rootDir) == 0 {
			continue
		}

		if _, err := os.Stat(rootDir); err != nil {
			log.Printf("config directory is not accessible: %v", rootDir)
			continue
		}
		err = symWalk(rootDir, rootDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			absPath, _ := filepath.Abs(path)
			relPath, _ := filepath.Rel(rootDir, filepath.Dir(path))

			if info.IsDir() {
				configOnDemand := filepath.Join(path, "config.on_demand")
				if _, e := os.Stat(configOnDemand); e == nil {
					if relPath == ".." {
						relPath = "."
					}
					if _, found := configMap[relPath]; !found {
						configMap[relPath] = nil
					}
					return filepath.SkipDir
				}
			}

			if !info.IsDir() && info.Name() == "config.json" {
				if strings.HasSuffix(rootDir, "config.json") && relPath == ".." {
					relPath = "."
				}

				if _, found := configMap[relPath]; found {
					return nil
				}

				if verbose {
					log.Printf("Loading config file: %s under namespace: %s\n", absPath, relPath)
				}

				config := &Config{}
				e := config.LoadConfigFile(absPath, verbose)
				if e != nil {
					return e
				}

				configMap[relPath] = config
				e = config.postprocessConfig(relPath)
				if e != nil {
					return e
				}
			}
			return nil
		})

		if err != nil {
			return nil, err
		}
	}

	if err == nil && len(configMap) == 0 {
		err = fmt.Errorf("No config file found")
		return nil, err
	}

	for _, config := range configMap {
		if config == nil {
			continue
		}
		for i := range config.Layers {
			err = config.processFusionTimestamps(i, configMap)
			if err != nil {
				return nil, err
			}
			err = config.processFusionColourPalette(i, configMap)
			if err != nil {
				return nil, err
			}
		}
		PostprocessServiceConfig(config, configMap, verbose)
	}

	return configMap, err
}

func LoadConfigTimestamps(config *Config, verbose bool) error {
	for iLayer := range config.Layers {
		config.GetLayerDates(iLayer, verbose)
	}

	ns := config.ServiceConfig.NameSpace
	if len(ns) == 0 {
		ns = "."
	}
	confMap := make(map[string]*Config)
	confMap[ns] = config
	for iLayer := range config.Layers {
		err := config.processFusionTimestamps(iLayer, confMap)
		if err != nil {
			return err
		}
	}
	return nil
}

func PostprocessServiceConfig(config *Config, confMap map[string]*Config, verbose bool) {
	namespace := strings.TrimSpace(config.ServiceConfig.NameSpace)
	if len(namespace) == 0 || namespace == "." {
		return
	}

	if config.hasClusterInfo() {
		return
	}

	ns := strings.TrimRight(namespace, "/")
	for {
		idx := strings.LastIndex(ns, "/")
		if idx < 0 {
			break
		}
		ns = ns[:idx]

		if conf, found := confMap[ns]; found {
			if conf != nil && conf.hasClusterInfo() {
				config.copyClusterInfo(conf)
				return
			}
		}
	}

	if conf, found := confMap["."]; found {
		if conf == nil {
			cm, err := LoadConfigOnDemand(EtcDir, ".", verbose)
			if err == nil {
				conf = cm["."]
			}
		}
		if conf != nil && conf.hasClusterInfo() {
			config.copyClusterInfo(conf)
		}
	}
}

func (config *Config) hasClusterInfo() bool {
	hasClusterInfo := true
	if len(strings.TrimSpace(config.ServiceConfig.MASAddress)) == 0 {
		hasClusterInfo = false
	}
	if len(config.ServiceConfig.WorkerNodes) == 0 {
		hasClusterInfo = false
	}
	return hasClusterInfo
}

func (config *Config) copyClusterInfo(conf *Config) {
	if len(config.ServiceConfig.MASAddress) == 0 {
		config.ServiceConfig.MASAddress = conf.ServiceConfig.MASAddress
		for i := range config.Layers {
			if len(config.Layers[i].MASAddress) == 0 {
				config.Layers[i].MASAddress = config.ServiceConfig.MASAddress
			}
			if len(config.Layers[i].Overviews) > 0 {
				for ii := range config.Layers[i].Overviews {
					if len(config.Layers[i].Overviews[ii].MASAddress) == 0 {
						config.Layers[i].Overviews[ii].MASAddress = config.Layers[i].MASAddress
					}
				}
			}
			for j := range config.Layers[i].Styles {
				if len(config.Layers[i].Styles[j].MASAddress) == 0 {
					config.Layers[i].Styles[j].MASAddress = config.Layers[i].MASAddress
				}
			}
		}
	}
	if len(config.ServiceConfig.WorkerNodes) == 0 {
		config.ServiceConfig.WorkerNodes = conf.ServiceConfig.WorkerNodes
	}
}

// Unmarshal is wrapper around json.Unmarshal that returns user-friendly
// errors when there are syntax errors.
// https://github.com/hashicorp/packer/blob/master/common/json/unmarshal.go
func Unmarshal(data []byte, i interface{}) error {
	err := json.Unmarshal(data, i)
	if err != nil {
		syntaxErr, ok := err.(*json.SyntaxError)
		if !ok {
			return err
		}

		// We have a syntax error. Extract out the line number and friends.
		// https://groups.google.com/forum/#!topic/golang-nuts/fizimmXtVfc
		newline := []byte{'\x0a'}

		// Calculate the start/end position of the line where the error is
		start := bytes.LastIndex(data[:syntaxErr.Offset], newline) + 1
		end := len(data)
		if idx := bytes.Index(data[start:], newline); idx >= 0 {
			end = start + idx
		}

		// Count the line number we're on plus the offset in the line
		line := bytes.Count(data[:start], newline) + 1
		pos := int(syntaxErr.Offset) - start - 1

		err = fmt.Errorf("Error in line %d, char %d: %s\n%s",
			line, pos, syntaxErr, data[start:end])
		return err
	}

	return nil
}

func (config *Config) postprocessConfig(ns string) error {
	for i := range config.Layers {
		if ns == "." {
			ns = ""
		}

		config.ServiceConfig.NameSpace = ns
		if len(config.Layers[i].MASAddress) == 0 {
			config.Layers[i].MASAddress = config.ServiceConfig.MASAddress
		}

		if len(config.Layers[i].Overviews) > 0 {
			for ii, ovr := range config.Layers[i].Overviews {
				if len(ovr.DataSource) == 0 {
					return fmt.Errorf("%s, %s: overview[%d] has no data_source", config.Layers[i].Name, ns, ii)
				}

				if ovr.ZoomLimit <= 0 {
					return fmt.Errorf("%s, %s: overview[%d] has no zoom_limit", config.Layers[i].Name, ns, ii)
				}

				if len(config.Layers[i].Overviews[ii].MASAddress) == 0 {
					config.Layers[i].Overviews[ii].MASAddress = config.Layers[i].MASAddress
				}
			}
			sort.Slice(config.Layers[i].Overviews, func(m, n int) bool { return config.Layers[m].ZoomLimit < config.Layers[n].ZoomLimit })
		}
		config.Layers[i].NameSpace = ns
		for j := range config.Layers[i].Styles {
			config.Layers[i].Styles[j].OWSHostname = config.Layers[i].OWSHostname
			config.Layers[i].Styles[j].NameSpace = config.Layers[i].NameSpace
			if len(config.Layers[i].Styles[j].DataSource) == 0 {
				config.Layers[i].Styles[j].DataSource = config.Layers[i].DataSource
			}
			if len(config.Layers[i].Styles[j].MASAddress) == 0 {
				config.Layers[i].Styles[j].MASAddress = config.Layers[i].MASAddress
			}
			if config.Layers[i].Styles[j].LegendWidth <= 0 {
				config.Layers[i].Styles[j].LegendWidth = DefaultLegendWidth
			}
			if config.Layers[i].Styles[j].LegendHeight <= 0 {
				config.Layers[i].Styles[j].LegendHeight = DefaultLegendHeight
			}

			bandExpr, err := ParseBandExpressions(config.Layers[i].Styles[j].RGBProducts)
			if err != nil {
				return fmt.Errorf("Layer %v, style %v, RGBExpression parsing error: %v", config.Layers[i].Name, config.Layers[i].Styles[j].Name, err)
			}
			config.Layers[i].Styles[j].RGBExpressions = bandExpr

			if len(config.Layers[i].Styles[j].FeatureInfoBands) > 0 {
				featureInfoExpr, err := ParseBandExpressions(config.Layers[i].Styles[j].FeatureInfoBands)
				if err != nil {
					return fmt.Errorf("Layer %v, style %v, FeatureInfoExpression parsing error: %v", config.Layers[i].Name, config.Layers[i].Styles[j].Name, err)
				}
				config.Layers[i].Styles[j].FeatureInfoExpressions = featureInfoExpr
			}

			if len(config.Layers[i].Styles[j].InputLayers) == 0 && len(config.Layers[i].InputLayers) > 0 {
				config.Layers[i].Styles[j].InputLayers = config.Layers[i].InputLayers
			}

			if len(config.Layers[i].Styles[j].InputLayers) > 0 {
				for k := range config.Layers[i].Styles[j].InputLayers {
					if len(config.Layers[i].Styles[j].InputLayers[k].Name) == 0 {
						config.Layers[i].Styles[j].InputLayers[k].Name = config.Layers[i].Name
					}
				}
			}

			if len(config.Layers[i].Styles[j].DisableServices) == 0 && len(config.Layers[i].DisableServices) > 0 {
				config.Layers[i].Styles[j].DisableServices = config.Layers[i].DisableServices
			}

			if len(config.Layers[i].Styles[j].Overviews) == 0 && len(config.Layers[i].Overviews) > 0 {
				config.Layers[i].Styles[j].Overviews = config.Layers[i].Overviews
			}

			if config.Layers[i].Styles[j].ZoomLimit == 0.0 && config.Layers[i].ZoomLimit != 0.0 {
				config.Layers[i].Styles[j].ZoomLimit = config.Layers[i].ZoomLimit
			}

			if !strings.HasPrefix(config.Layers[i].Styles[j].Name, "__") {
				config.Layers[i].Styles[j].Visibility = "visible"
			}
		}
	}

	return nil
}

func hasBlendedService(layer *Layer) bool {
	if len(layer.InputLayers) > 0 && len(strings.TrimSpace(layer.DataSource)) == 0 {
		return true
	}

	if len(layer.Styles) > 0 && len(layer.Styles[0].InputLayers) > 0 {
		return true
	}

	return false
}

func (config *Config) getFusionRefLayer(i int, refLayer *Layer, configMap map[string]*Config) (int, int, string, error) {
	refNameSpace := refLayer.NameSpace
	if len(refNameSpace) == 0 {
		if len(config.Layers[i].NameSpace) == 0 {
			refNameSpace = "."
		} else {
			refNameSpace = config.Layers[i].NameSpace
		}
	}

	conf, found := configMap[refNameSpace]
	if !found {
		return -1, -1, "", fmt.Errorf("namespace %s not found referenced by %s", refNameSpace, refLayer.Name)
	}

	params := WMSParams{Layers: []string{refLayer.Name}}
	layerIdx, err := GetLayerIndex(params, conf)
	if err != nil {
		return -1, -1, "", err
	}

	styleIdx := -1
	if len(refLayer.Styles) > 0 {
		styleParams := WMSParams{Styles: []string{refLayer.Styles[0].Name}}
		styleIdx, err = GetLayerStyleIndex(styleParams, conf, layerIdx)
		if err != nil {
			return layerIdx, -1, "", err
		}
	}

	return layerIdx, styleIdx, refNameSpace, nil
}

func (config *Config) processFusionTimestamps(i int, configMap map[string]*Config) error {
	var inputLayers []Layer
	if len(config.Layers[i].InputLayers) > 0 {
		inputLayers = config.Layers[i].InputLayers
	} else if len(config.Layers[i].Styles) > 0 && len(config.Layers[i].Styles[0].InputLayers) > 0 {
		inputLayers = config.Layers[i].Styles[0].InputLayers
	}
	if len(inputLayers) > 0 {
		var timestamps []string
		tsLookup := make(map[string]struct{})
		for _, dt := range config.Layers[i].Dates {
			if _, found := tsLookup[dt]; !found {
				tsLookup[dt] = struct{}{}
				timestamps = append(timestamps, dt)
			}
		}

		for _, refLayer := range inputLayers {
			layerIdx, _, refNameSpace, err := config.getFusionRefLayer(i, &refLayer, configMap)
			if err != nil {
				return err
			}
			layer := &configMap[refNameSpace].Layers[layerIdx]
			if hasBlendedService(layer) && len(layer.Dates) == 0 && len(strings.TrimSpace(layer.EffectiveStartDate)) == 0 && len(strings.TrimSpace(layer.EffectiveEndDate)) == 0 {
				err := config.processFusionTimestamps(layerIdx, configMap)
				if err != nil {
					return err
				}
			}
			for _, dt := range layer.Dates {
				if _, found := tsLookup[dt]; !found {
					tsLookup[dt] = struct{}{}
					timestamps = append(timestamps, dt)
				}
			}
		}

		sort.Slice(timestamps, func(i, j int) bool {
			t1, _ := time.Parse(ISOFormat, timestamps[i])
			t2, _ := time.Parse(ISOFormat, timestamps[j])
			return t1.Before(t2)
		})

		if len(timestamps) > 0 {
			config.Layers[i].Dates = timestamps
			config.Layers[i].EffectiveStartDate = timestamps[0]
			config.Layers[i].EffectiveEndDate = timestamps[len(timestamps)-1]
		}
	}
	return nil
}

func (config *Config) processFusionColourPalette(i int, configMap map[string]*Config) error {
	var inputLayers []Layer
	if len(config.Layers[i].InputLayers) > 0 {
		inputLayers = config.Layers[i].InputLayers
	} else if len(config.Layers[i].Styles) > 0 && len(config.Layers[i].Styles[0].InputLayers) > 0 {
		inputLayers = config.Layers[i].Styles[0].InputLayers
	}

	if len(inputLayers) > 0 {
		if len(config.Layers[i].Styles) == 0 {
			if len(config.Layers[i].RGBProducts) != 1 {
				return nil
			}
			if config.Layers[i].Palette != nil {
				return nil
			}

			refLayer := config.Layers[i].InputLayers[0]
			layerIdx, styleIdx, refNameSpace, err := config.getFusionRefLayer(i, &refLayer, configMap)
			if err != nil {
				return err
			}

			layer := &configMap[refNameSpace].Layers[layerIdx]
			layerBase := layer
			if styleIdx >= 0 {
				layer = &configMap[refNameSpace].Layers[layerIdx].Styles[styleIdx]
			}

			if hasBlendedService(layerBase) && layer.Palette == nil {
				err := config.processFusionColourPalette(layerIdx, configMap)
				if err != nil {
					return err
				}
			}

			config.Layers[i].Palette = layer.Palette
		} else {
			for j := range config.Layers[i].Styles {
				if len(config.Layers[i].Styles[j].RGBProducts) != 1 {
					continue
				}
				if config.Layers[i].Styles[j].Palette != nil {
					continue
				}

				refLayer := config.Layers[i].Styles[j].InputLayers[0]
				layerIdx, styleIdx, refNameSpace, err := config.getFusionRefLayer(i, &refLayer, configMap)
				if err != nil {
					return err
				}
				layer := &configMap[refNameSpace].Layers[layerIdx]
				layerBase := layer
				if styleIdx >= 0 {
					layer = &configMap[refNameSpace].Layers[layerIdx].Styles[styleIdx]
				}

				if hasBlendedService(layerBase) && layer.Palette == nil {
					err := config.processFusionColourPalette(layerIdx, configMap)
					if err != nil {
						return err
					}
				}

				config.Layers[i].Styles[j].Palette = layer.Palette
			}
		}
	}
	return nil
}

// CopyConfig makes a deep copy of the certain fields of the config object.
// For the time being, we only copy the fields required for GetCapabilities.
func (config *Config) Copy(r *http.Request) *Config {
	newConf := &Config{}
	newConf.ServiceConfig = ServiceConfig{
		OWSHostname: config.ServiceConfig.OWSHostname,
		OWSProtocol: config.ServiceConfig.OWSProtocol,
		NameSpace:   config.ServiceConfig.NameSpace,
		MASAddress:  config.ServiceConfig.MASAddress,
	}

	hasOWSHostname := len(strings.TrimSpace(config.ServiceConfig.OWSHostname)) > 0
	if !hasOWSHostname {
		newConf.ServiceConfig.OWSHostname = r.Host
	}

	hasOWSProtocol := len(strings.TrimSpace(config.ServiceConfig.OWSProtocol)) > 0
	if !hasOWSProtocol {
		newConf.ServiceConfig.OWSProtocol = ParseRequestProtocol(r)
	}

	newConf.Layers = make([]Layer, len(config.Layers))
	for i, layer := range config.Layers {
		if hasBlendedService(&layer) {
			newConf.Layers[i] = layer
			continue
		}
		newConf.Layers[i] = Layer{
			Name:               layer.Name,
			Title:              layer.Title,
			Abstract:           layer.Abstract,
			NameSpace:          layer.NameSpace,
			OWSHostname:        layer.OWSHostname,
			OWSProtocol:        newConf.ServiceConfig.OWSProtocol,
			Styles:             layer.Styles,
			AxesInfo:           layer.AxesInfo,
			StepDays:           layer.StepDays,
			StepHours:          layer.StepHours,
			StepMinutes:        layer.StepMinutes,
			StartISODate:       layer.StartISODate,
			EndISODate:         layer.EndISODate,
			TimeGen:            layer.TimeGen,
			Accum:              layer.Accum,
			DataSource:         layer.DataSource,
			RGBExpressions:     layer.RGBExpressions,
			TimestampToken:     layer.TimestampToken,
			Dates:              layer.Dates,
			EffectiveStartDate: layer.EffectiveStartDate,
			EffectiveEndDate:   layer.EffectiveEndDate,
		}
		if !hasOWSHostname {
			newConf.Layers[i].OWSHostname = r.Host
		}
	}

	newConf.Processes = make([]Process, len(config.Processes))
	for i, proc := range config.Processes {
		newConf.Processes[i] = proc
	}

	newConf.Extensions = make([]CapabilityExtension, len(config.Extensions))
	for i, ext := range config.Extensions {
		newConf.Extensions[i] = ext
	}

	newConf.WmsGeojsonClipConfig = WmsGeojsonClipConfig{
		GeojsonGetEndpoint: config.WmsGeojsonClipConfig.GeojsonGetEndpoint,
	}

	return newConf
}

// GetLayerDates loads dates for the ith layer
func (config *Config) GetLayerDates(iLayer int, verbose bool) {
	layer := config.Layers[iLayer]
	step := time.Minute * time.Duration(60*24*layer.StepDays+60*layer.StepHours+layer.StepMinutes)

	if strings.TrimSpace(strings.ToLower(layer.TimeGen)) == "mas" {
		if hasBlendedService(&layer) {
			return
		}

		timestamps, token := GenerateDatesMas(layer.StartISODate, layer.EndISODate, config.ServiceConfig.MASAddress, layer.DataSource, layer.RGBExpressions.VarList, step, layer.TimestampToken, verbose)
		if len(timestamps) > 0 && len(token) > 0 {
			config.Layers[iLayer].Dates = timestamps
			config.Layers[iLayer].TimestampToken = token
		} else if len(timestamps) == 0 && len(token) > 0 {
			if verbose {
				log.Printf("Cached %d timestamps", len(config.Layers[iLayer].Dates))
			}
			config.Layers[iLayer].TimestampToken = token
			return
		} else {
			log.Printf("Failed to get MAS timestamps, layer: %s", layer.Name)
			return
		}
	} else {
		startDate := layer.StartISODate
		endDate := layer.EndISODate

		useMasTimestamps := false
		if strings.TrimSpace(strings.ToLower(startDate)) == "mas" {
			useMasTimestamps = true
			startDate = ""
		}

		if strings.TrimSpace(strings.ToLower(endDate)) == "mas" {
			useMasTimestamps = true
			endDate = ""
		} else if strings.TrimSpace(strings.ToLower(endDate)) == "now" {
			endDate = time.Now().UTC().Format(ISOFormat)
		}

		if useMasTimestamps {
			if hasBlendedService(&layer) {
				return
			}

			masTimestamps, token := GenerateDatesMas(startDate, endDate, config.ServiceConfig.MASAddress, layer.DataSource, layer.RGBExpressions.VarList, 0, layer.TimestampToken, verbose)
			if len(token) == 0 {
				log.Printf("Failed to get MAS timestamps, layer: %s", layer.Name)
				return
			} else if len(masTimestamps) == 0 && len(token) > 0 {
				if verbose {
					log.Printf("Cached %d timestamps", len(config.Layers[iLayer].Dates))
				}
				config.Layers[iLayer].TimestampToken = token
				return
			}
			config.Layers[iLayer].TimestampToken = token

			if len(startDate) == 0 {
				startDate = masTimestamps[0]
			}

			if len(endDate) == 0 {
				endDate = masTimestamps[len(masTimestamps)-1]
			}
		}

		start, errStart := time.Parse(ISOFormat, startDate)
		if errStart != nil {
			log.Printf("start date parsing error: %v, layer: %s", errStart, layer.Name)
			return
		}

		end, errEnd := time.Parse(ISOFormat, endDate)
		if errEnd != nil {
			log.Printf("end date parsing error: %v, layer: %s", errEnd, layer.Name)
			return
		}

		if useMasTimestamps && step > 0 {
			// We normalise the timestamps by truncating them up to the required precision.
			// The truncation process essentially rounds down the datetime to the
			// nearest precision from the left. This implies that we should not
			// do such a normalisation to the end datetime. Or we might miss
			// out some data points due to lower time resolution on the upper time
			// end point.
			// This behaviour is also consistent with the manual timestep generator
			// which offers open-ended upper time end point.
			if layer.StepDays > 0 {
				start = start.Truncate(24 * 60 * time.Minute)
			} else if layer.StepHours > 0 {
				start = start.Truncate(60 * time.Minute)
			} else if layer.StepMinutes > 0 {
				start = start.Truncate(time.Minute)
			}

			if verbose {
				log.Printf("Normalised MAS start date: %v", start.Format(ISOFormat))
			}
		}

		if start == end {
			config.Layers[iLayer].Dates = append(config.Layers[iLayer].Dates, start.Format(ISOFormat))
		} else {
			config.Layers[iLayer].Dates = GenerateDates(layer.TimeGen, start, end, step)
		}
	}

	nDates := len(config.Layers[iLayer].Dates)
	if nDates > 0 {
		config.Layers[iLayer].EffectiveStartDate = config.Layers[iLayer].Dates[0]
		config.Layers[iLayer].EffectiveEndDate = config.Layers[iLayer].Dates[nDates-1]
	}
}

func CheckBandExpressionsComplexity(bandExpr *BandExpressions, criteria *BandExpressionComplexityCriteria) error {
	errPrefix := "band math error"
	if len(criteria.VariableLookup) == 0 {
		return fmt.Errorf("%s: user-defined band math is not enabled for this layer", errPrefix)
	}

	if len(bandExpr.Expressions) > criteria.MaxExpressions {
		return fmt.Errorf("%s: Too many expressions: %d", errPrefix, len(bandExpr.Expressions))
	}

	if len(bandExpr.VarList) > criteria.MaxVariables {
		return fmt.Errorf("%s: Too many variables: %d", errPrefix, len(bandExpr.VarList))
	}

	tokenCount := 0
	for _, expr := range bandExpr.Expressions {
		tokenCount += len(expr.Tokens())
	}
	if tokenCount > criteria.MaxTokens {
		return fmt.Errorf("%s: Too many tokens: %d", errPrefix, tokenCount)
	}

	for _, expr := range bandExpr.Expressions {
		for _, token := range expr.Tokens() {
			if token.Kind == goeval.VARIABLE {
				varName, ok := token.Value.(string)
				if !ok {
					return fmt.Errorf("%s: variable token '%v' failed to cast string", errPrefix, token.Value)
				}

				if _, found := criteria.VariableLookup[varName]; !found {
					var varNames []string
					for v, _ := range criteria.VariableLookup {
						varNames = append(varNames, v)
					}
					return fmt.Errorf("%s: variable not supported: %s, supported variables are: %s", errPrefix, varName, strings.Join(varNames, ", "))
				}
			}
		}
	}

	if len(criteria.TokenACL) > 0 {
		aclLookup := make(map[string]map[string]struct{})
		for _, expr := range bandExpr.Expressions {
			for _, token := range expr.Tokens() {
				tokenKind := token.Kind.String()
				acl, found := criteria.TokenACL[tokenKind]
				if !found {
					continue
				}

				isTokenAllowed := true
				switch t := acl.(type) {
				case nil:
					isTokenAllowed = false
				case []interface{}:
					if _, found := aclLookup[tokenKind]; !found {
						aclLookup[tokenKind] = make(map[string]struct{})
						for _, v := range t {
							vs, ok := v.(string)
							if !ok {
								log.Printf("%s: ACL value is not string: %#s", errPrefix, acl)
							}
							aclLookup[tokenKind][vs] = struct{}{}
						}
					}
					val, ok := token.Value.(string)
					if !ok {
						log.Printf("%s: token value is not string: %#s", errPrefix, token.Value)
						continue
					}
					if _, found := aclLookup[tokenKind][val]; found {
						isTokenAllowed = false
					}
				default:
					log.Printf("%s: unknown ACL: %#s", errPrefix, acl)
				}

				if !isTokenAllowed {
					return fmt.Errorf("%s: Operation not supported: %v", errPrefix, token.Value)
				}
			}
		}
	}
	return nil
}

func ParseBandExpressions(bands []string) (*BandExpressions, error) {
	bandExpr := &BandExpressions{ExprText: bands}
	varFound := make(map[string]struct{})
	hasExprAll := false
	for ib, bandRaw := range bands {
		parts := strings.Split(bandRaw, "=")
		if len(parts) == 0 {
			return nil, fmt.Errorf("invalid expression: %v", bandRaw)
		}
		for ip, p := range parts {
			parts[ip] = strings.TrimSpace(p)
			if len(parts[ip]) == 0 {
				return nil, fmt.Errorf("invalid expression: %v", bandRaw)
			}
		}
		var band string
		if len(parts) == 1 {
			band = parts[0]
		} else if len(parts) == 2 {
			band = parts[1]
		} else {
			return nil, fmt.Errorf("invalid expression: %v", bandRaw)
		}

		expr, err := goeval.NewEvaluableExpression(band)
		if err != nil {
			return nil, err
		}
		bandExpr.Expressions = append(bandExpr.Expressions, expr)

		bandExpr.ExprVarRef = append(bandExpr.ExprVarRef, []string{})
		bandVarFound := make(map[string]struct{})
		for _, token := range expr.Tokens() {
			if token.Kind == goeval.VARIABLE {
				varName, ok := token.Value.(string)
				if !ok {
					return nil, fmt.Errorf("variable token '%v' failed to cast string for band '%v'", token.Value, band)
				}

				if _, found := varFound[varName]; !found {
					varFound[varName] = struct{}{}
					bandExpr.VarList = append(bandExpr.VarList, varName)
				}

				if _, found := bandVarFound[varName]; !found {
					bandVarFound[varName] = struct{}{}
					bandExpr.ExprVarRef[ib] = append(bandExpr.ExprVarRef[ib], varName)
				}

			} else {
				hasExprAll = true
			}
		}

		if len(parts) == 1 {
			bandExpr.ExprNames = append(bandExpr.ExprNames, band)
		} else if len(parts) == 2 {
			bandExpr.ExprNames = append(bandExpr.ExprNames, parts[0])
		}
	}

	if !hasExprAll {
		bandExpr.Expressions = nil
	}
	return bandExpr, nil
}

// LoadConfigFileTemplate parses the config as a Jet
// template and escapes any GSKY here docs (i.e. $gdoc$)
// into valid one-line JSON strings.
func LoadConfigFileTemplate(configFile string) ([]byte, error) {
	path := filepath.Dir(configFile)

	view := jet.NewSet(jet.SafeWriter(func(w io.Writer, b []byte) {
		w.Write(b)
	}), path, "/")

	template, err := view.GetTemplate(configFile)
	if err != nil {
		return nil, err
	}

	var resBuf bytes.Buffer
	vars := make(jet.VarMap)
	if err = template.Execute(&resBuf, vars, nil); err != nil {
		return nil, err
	}

	gdocSym := `$gdoc$`

	// JSON escape rules: https://www.freeformatter.com/json-escape.html
	escapeRules := func(str string) string {
		tokens := []string{"\b", "\f", "\n", "\r", "\t", `"`}
		repl := []string{`\b`, `\f`, `\n`, `\r`, `\t`, `\"`}

		str = strings.Replace(str, `\`, `\\`, -1)
		for it, t := range tokens {
			str = strings.Replace(str, t, repl[it], -1)
		}
		str = `"` + str + `"`
		return str
	}

	rawStr := resBuf.String()
	nHereDocs := strings.Count(rawStr, gdocSym)
	if nHereDocs == 0 {
		return []byte(rawStr), nil
	}

	if nHereDocs%2 != 0 {
		return nil, fmt.Errorf("gdocs are not properly closed")
	}

	strParts := strings.Split(rawStr, gdocSym)

	var escapedStr string
	for ip, part := range strParts {
		if ip%2 == 0 {
			escapedStr += part
		} else {
			escapedStr += escapeRules(part)
		}
	}

	return []byte(escapedStr), nil
}

func getGrpcPoolSize(config *Config, verbose bool) int {
	opts := []grpc.DialOption{
		grpc.WithInsecure(),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(DefaultRecvMsgSize)),
	}

	workerNodes := config.ServiceConfig.WorkerNodes
	var connPool []*grpc.ClientConn
	var effectiveWorkerNodes []string
	for i := 0; i < len(workerNodes); i++ {
		conn, err := grpc.Dial(workerNodes[i], opts...)
		if err != nil {
			log.Printf("gRPC connection problem: %v", err)
			continue
		}
		defer conn.Close()

		connPool = append(connPool, conn)
		effectiveWorkerNodes = append(effectiveWorkerNodes, workerNodes[i])
	}

	var wg sync.WaitGroup
	wg.Add(len(connPool))

	concLimit := make(chan bool, DefaultConcGrpcWorkerQuery)
	workerPoolSizes := make([]int, len(connPool))
	for i := 0; i < len(connPool); i++ {
		concLimit <- true
		go func(i int) {
			defer wg.Done()
			defer func() { <-concLimit }()
			c := pb.NewGDALClient(connPool[i])
			req := &pb.GeoRPCGranule{Operation: "worker_info"}

			ctx := context.Background()
			ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
			r, err := c.Process(ctx, req)
			cancel()
			if err == nil {
				workerPoolSizes[i] = int(r.WorkerInfo.PoolSize)
			} else {
				if verbose {
					log.Printf("Failed to query gRPC worker %s, %v", effectiveWorkerNodes[i], err)
				}
			}
		}(i)
	}
	wg.Wait()

	avgPoolSize := 0.0
	cnt := 0.0
	for _, ps := range workerPoolSizes {
		if ps > 0 {
			avgPoolSize += float64(ps)
			cnt++
		}
	}

	if cnt >= 1 {
		avgPoolSize /= cnt
	}

	return int(avgPoolSize + 0.5)
}

func addBandMathVariableConstraints(config *Config, layer *Layer, criteria *BandExpressionComplexityCriteria) {
	criteria.VariableLookup = make(map[string]struct{})

	varToken := "VARIABLE"
	if _, found := criteria.TokenACL[varToken]; found {
		acl, ok := criteria.TokenACL[varToken].([]interface{})
		if ok {
			for _, _v := range acl {
				v, ok := _v.(string)
				if !ok {
					continue
				}
				criteria.VariableLookup[v] = struct{}{}
			}
		}
	}

	for _, ext := range config.Extensions {
		if ext.Name != "user_band_math" {
			continue
		}
		if ext.Layer.Name != layer.Name {
			continue
		}

		for _, prop := range ext.Properties {
			if prop.Name != "available_bands" {
				continue
			}
			criteria.VariableLookup[prop.Value] = struct{}{}
		}

	}
}

// LoadConfigFile marshalls the config.json document returning an
// instance of a Config variable containing all the values
func (config *Config) LoadConfigFile(configFile string, verbose bool) error {
	cfg, err := LoadConfigFileTemplate(configFile)
	if err != nil {
		return fmt.Errorf("Error while reading config file: %s. Error: %v", configFile, err)
	}

	return config.LoadConfigString(cfg, verbose)
}

func (config *Config) LoadConfigString(cfg []byte, verbose bool) error {
	err := Unmarshal(cfg, config)
	if err != nil {
		return fmt.Errorf("Error at JSON parsing config document: %v", err)
	}

	if len(config.ServiceConfig.TempDir) > 0 {
		if verbose {
			log.Printf("Creating temp directory: %v", config.ServiceConfig.TempDir)
		}
		err := os.MkdirAll(config.ServiceConfig.TempDir, os.ModePerm)
		if err != nil {
			return fmt.Errorf("error creating temp directory: %v", err)
		}
	}

	if config.ServiceConfig.MaxGrpcBufferSize > 0 && config.ServiceConfig.MaxGrpcBufferSize < 10 {
		config.ServiceConfig.MaxGrpcBufferSize = 0
		log.Printf("MaxGrpcBufferSize is set to less than 10MB, reset to unlimited")
	}

	config.ServiceConfig.MaxGrpcBufferSize = config.ServiceConfig.MaxGrpcBufferSize * 1024 * 1024

	grpcPoolSize := getGrpcPoolSize(config, verbose)
	if verbose {
		log.Printf("average grpc worker pool size: %d", grpcPoolSize)
	}

	fileResolver := NewRuntimeFileResolver(DataDir)
	resolveFilePath := func(filePath, layerName string) string {
		path, err := fileResolver.Resolve(filePath)
		if err != nil {
			log.Printf("File resolution error: layer:%v, err:%v", layerName, err)
		}
		return path
	}
	for i, layer := range config.Layers {
		bandExpr, err := ParseBandExpressions(layer.RGBProducts)
		if err != nil {
			return fmt.Errorf("Layer %v RGBExpression parsing error: %v", layer.Name, err)
		}
		config.Layers[i].RGBExpressions = bandExpr

		featureInfoExpr, err := ParseBandExpressions(layer.FeatureInfoBands)
		if err != nil {
			return fmt.Errorf("Layer %v FeatureInfoExpression parsing error: %v", layer.Name, err)
		}
		config.Layers[i].FeatureInfoExpressions = featureInfoExpr

		if len(strings.TrimSpace(config.Layers[i].TimestampsLoadStrategy)) == 0 {
			config.Layers[i].TimestampsLoadStrategy = "on_demand"
		}
		if config.Layers[i].TimestampsLoadStrategy != "on_demand" {
			config.GetLayerDates(i, verbose)
		}

		config.Layers[i].OWSHostname = config.ServiceConfig.OWSHostname

		if config.Layers[i].MaxGrpcRecvMsgSize <= DefaultRecvMsgSize {
			config.Layers[i].MaxGrpcRecvMsgSize = DefaultRecvMsgSize
		}

		if config.Layers[i].WmsPolygonSegments <= DefaultWmsPolygonSegments {
			config.Layers[i].WmsPolygonSegments = DefaultWmsPolygonSegments
		}

		if config.Layers[i].WcsPolygonSegments <= DefaultWcsPolygonSegments {
			config.Layers[i].WcsPolygonSegments = DefaultWcsPolygonSegments
		}

		if config.Layers[i].WmsTimeout <= 0 {
			config.Layers[i].WmsTimeout = DefaultWmsTimeout
		}

		if config.Layers[i].WcsTimeout <= 0 {
			config.Layers[i].WcsTimeout = DefaultWcsTimeout
		}

		if config.Layers[i].GrpcWmsConcPerNode <= 0 {
			conc := grpcPoolSize
			if conc < DefaultGrpcWmsConcPerNode {
				conc = DefaultGrpcWmsConcPerNode
			}
			config.Layers[i].GrpcWmsConcPerNode = conc
		}

		if config.Layers[i].GrpcWcsConcPerNode <= 0 {
			conc := grpcPoolSize
			if conc < DefaultGrpcWcsConcPerNode {
				conc = DefaultGrpcWcsConcPerNode
			}
			config.Layers[i].GrpcWcsConcPerNode = conc
		}

		if config.Layers[i].WmsPolygonShardConcLimit <= 0 {
			config.Layers[i].WmsPolygonShardConcLimit = DefaultWmsPolygonShardConcLimit
		}

		if config.Layers[i].WcsPolygonShardConcLimit <= 0 {
			config.Layers[i].WcsPolygonShardConcLimit = DefaultWcsPolygonShardConcLimit
		}

		if layer.Palette != nil && layer.Palette.Colours != nil && len(layer.Palette.Colours) < 3 {
			return fmt.Errorf("The colour palette must contain at least 2 colours.")
		}

		if config.Layers[i].WmsMaxWidth <= 0 {
			config.Layers[i].WmsMaxWidth = DefaultWmsMaxWidth
		}

		if config.Layers[i].WmsMaxHeight <= 0 {
			config.Layers[i].WmsMaxHeight = DefaultWmsMaxHeight
		}

		if config.Layers[i].WcsMaxWidth <= 0 {
			config.Layers[i].WcsMaxWidth = DefaultWcsMaxWidth
		}

		if config.Layers[i].WcsMaxHeight <= 0 {
			config.Layers[i].WcsMaxHeight = DefaultWcsMaxHeight
		}

		if config.Layers[i].WcsMaxTileWidth <= 0 {
			config.Layers[i].WcsMaxTileWidth = DefaultWcsMaxTileWidth
		}

		if config.Layers[i].WcsMaxTileHeight <= 0 {
			config.Layers[i].WcsMaxTileHeight = DefaultWcsMaxTileHeight
		}

		if config.Layers[i].WmsBandExpressionCriteria == nil {
			config.Layers[i].WmsBandExpressionCriteria = &BandExpressionComplexityCriteria{}
		}
		if config.Layers[i].WmsBandExpressionCriteria.MaxVariables <= 0 {
			config.Layers[i].WmsBandExpressionCriteria.MaxVariables = DefaultWmsMaxBandVariables
		}
		if config.Layers[i].WmsBandExpressionCriteria.MaxTokens <= 0 {
			config.Layers[i].WmsBandExpressionCriteria.MaxTokens = DefaultWmsMaxBandTokens
		}
		if config.Layers[i].WmsBandExpressionCriteria.MaxExpressions <= 0 {
			config.Layers[i].WmsBandExpressionCriteria.MaxExpressions = DefaultWmsMaxBandExpressions
		}
		addBandMathVariableConstraints(config, &config.Layers[i], config.Layers[i].WmsBandExpressionCriteria)

		if config.Layers[i].WcsBandExpressionCriteria == nil {
			config.Layers[i].WcsBandExpressionCriteria = &BandExpressionComplexityCriteria{}
		}
		if config.Layers[i].WcsBandExpressionCriteria.MaxVariables <= 0 {
			config.Layers[i].WcsBandExpressionCriteria.MaxVariables = DefaultWcsMaxBandVariables
		}
		if config.Layers[i].WcsBandExpressionCriteria.MaxTokens <= 0 {
			config.Layers[i].WcsBandExpressionCriteria.MaxTokens = DefaultWcsMaxBandTokens
		}
		if config.Layers[i].WcsBandExpressionCriteria.MaxExpressions <= 0 {
			config.Layers[i].WcsBandExpressionCriteria.MaxExpressions = DefaultWcsMaxBandExpressions
		}
		addBandMathVariableConstraints(config, &config.Layers[i], config.Layers[i].WcsBandExpressionCriteria)

		if len(config.Layers[i].LegendPath) > 0 {
			config.Layers[i].LegendPath = resolveFilePath(config.Layers[i].LegendPath, config.Layers[i].Name)
		}
		if len(config.Layers[i].NoDataLegendPath) > 0 {
			config.Layers[i].NoDataLegendPath = resolveFilePath(config.Layers[i].NoDataLegendPath, config.Layers[i].Name)
		}
		for iStyle := range config.Layers[i].Styles {
			if len(config.Layers[i].Styles[iStyle].LegendPath) > 0 {
				config.Layers[i].Styles[iStyle].LegendPath = resolveFilePath(config.Layers[i].Styles[iStyle].LegendPath, fmt.Sprintf("%s/%s", config.Layers[i].Name, config.Layers[i].Styles[iStyle].Name))
			}
		}

	}

	for i, proc := range config.Processes {
		if proc.IdentityTol <= 0 {
			config.Processes[i].IdentityTol = -1.0
		}

		if proc.DpTol <= 0 {
			config.Processes[i].DpTol = -1.0
		}

		if proc.Approx == nil {
			approx := true
			config.Processes[i].Approx = &approx
		}

		if proc.WpsTimeout <= 0 {
			config.Processes[i].WpsTimeout = DefaultWpsTimeout
		}

		for ids, ds := range proc.DataSources {
			bandExpr, err := ParseBandExpressions(ds.RGBProducts)
			if err != nil {
				return fmt.Errorf("Process %v, data source %v, RGBExpression parsing error: %v", proc.Identifier, ids, err)
			}
			config.Processes[i].DataSources[ids].RGBExpressions = bandExpr

			if ds.Mask != nil {
				maskBands := []string{ds.Mask.ID}
				bandExpr, err := ParseBandExpressions(maskBands)
				if err != nil {
					return fmt.Errorf("Process %v, data source %v, IDExpression parsing error: %v", proc.Identifier, ids, err)
				}
				config.Processes[i].DataSources[ids].Mask.IDExpressions = bandExpr
			}

			if ds.GrpcWpsConcPerNode <= 0 {
				conc := grpcPoolSize
				if conc < DefaultGrpcWpsConcPerNode {
					conc = DefaultGrpcWpsConcPerNode
				}
				config.Processes[i].DataSources[ids].GrpcWpsConcPerNode = conc
			}

			if len(ds.MetadataURL) > 0 {
				config.Processes[i].DataSources[ids].MetadataURL = resolveFilePath(config.Processes[i].DataSources[ids].MetadataURL, proc.Identifier)
			}

			if len(ds.VRTURL) > 0 {
				config.Processes[i].DataSources[ids].VRTURL = resolveFilePath(config.Processes[i].DataSources[ids].VRTURL, proc.Identifier)
			}
		}

	}
	return nil
}

func DumpConfig(configs map[string]*Config) (string, error) {
	configJson, err := json.MarshalIndent(configs, "", "    ")
	if err != nil {
		return "", err
	}

	return string(configJson), nil
}

func GetRootConfig(searchPath string, verbose bool) (*Config, error) {
	var config *Config

	searchPathList := strings.Split(searchPath, ":")
	for _, rootDir := range searchPathList {
		rootDir = strings.TrimSpace(rootDir)
		if len(rootDir) == 0 {
			continue
		}
		configFile := filepath.Join(rootDir, "config.json")
		if _, e := os.Stat(configFile); e != nil {
			continue
		}

		config = &Config{}
		err := config.LoadConfigFile(configFile, verbose)
		if err != nil {
			config = nil
			log.Printf("Loading root config error: %v", err)
			continue
		}
		break
	}

	if config == nil {
		return nil, fmt.Errorf("Root configs not found: %v", searchPath)
	}
	return config, nil
}

func WatchConfig(infoLog, errLog *log.Logger, configMap *sync.Map, verbose bool) {
	// Catch SIGHUP to automatically reload config
	sighup := make(chan os.Signal, 1)
	signal.Notify(sighup, syscall.SIGHUP)
	go func() {
		for {
			select {
			case <-sighup:
				infoLog.Println("Caught SIGHUP, reloading config...")
				confMap, err := LoadAllConfigFiles(EtcDir, verbose)
				if err != nil {
					errLog.Printf("Error in loading config files: %v\n", err)
					continue
				}
				configMap.Store("config", confMap)
			}
		}
	}()
}

func parseGeojson(filePath string) (*geojson.FeatureCollection, error) {

	var featureCol *geojson.FeatureCollection

	geojsonFile, err := os.Open(filePath)

	if err != nil {
		return featureCol, err
	}

	defer geojsonFile.Close()

	byteValue, _ := ioutil.ReadAll(geojsonFile)

	err = json.Unmarshal(byteValue, &featureCol)

	featureCol, err = geojson.UnmarshalFeatureCollection(byteValue)

	if err != nil {
		return featureCol, fmt.Errorf("problem unmarshalling geometry")
	}

	return featureCol, nil
}
