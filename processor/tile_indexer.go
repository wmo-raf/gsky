package processor

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"math/rand"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nci/gsky/utils"
)

type DatasetAxis struct {
	Name               string    `json:"name"`
	Params             []float64 `json:"params"`
	Strides            []int     `json:"strides"`
	Shape              []int     `json:"shape"`
	Grid               string    `json:"grid"`
	IntersectionIdx    []int
	IntersectionValues []float64
	Order              int
	Aggregate          int
}

type GeoLocInfo struct {
	XDSName     string `json:"x_ds_name"`
	XBand       int    `json:"x_band"`
	YDSName     string `json:"y_ds_name"`
	YBand       int    `json:"y_band"`
	LineOffset  int    `json:"line_offset"`
	PixelOffset int    `json:"pixel_offset"`
	LineStep    int    `json:"line_step"`
	PixelStep   int    `json:"pixel_step"`
}

type GDALDataset struct {
	RawPath      string         `json:"file_path"`
	DSName       string         `json:"ds_name"`
	NameSpace    string         `json:"namespace"`
	ArrayType    string         `json:"array_type"`
	SRS          string         `json:"srs"`
	GeoTransform []float64      `json:"geo_transform"`
	TimeStamps   []time.Time    `json:"timestamps"`
	Polygon      string         `json:"polygon"`
	Means        []float64      `json:"means"`
	SampleCounts []int          `json:"sample_counts"`
	NoData       float64        `json:"nodata"`
	Axes         []*DatasetAxis `json:"axes"`
	GeoLocation  *GeoLocInfo    `json:"geo_loc"`
	IsOutRange   bool
}

type MetadataResponse struct {
	Error        string         `json:"error"`
	GDALDatasets []*GDALDataset `json:"gdal"`
}

type TileIndexer struct {
	Context    context.Context
	In         chan *GeoTileRequest
	Out        chan *GeoTileGranule
	Error      chan error
	APIAddress string
	QueryLimit int
}

const DefaultMASConcQuery = 32
const DefaultMASMaxConnsPerHost = 256

func NewTileIndexer(ctx context.Context, apiAddr string, errChan chan error) *TileIndexer {
	return &TileIndexer{
		Context:    ctx,
		In:         make(chan *GeoTileRequest, 100),
		Out:        make(chan *GeoTileGranule, 8192),
		Error:      errChan,
		APIAddress: apiAddr,
	}
}

func BBox2WKT(bbox []float64) string {
	// BBox xMin, yMin, xMax, yMax
	return fmt.Sprintf("POLYGON ((%f %f, %f %f, %f %f, %f %f, %f %f))", bbox[0], bbox[1], bbox[2], bbox[1], bbox[2], bbox[3], bbox[0], bbox[3], bbox[0], bbox[1])
}

func (p *TileIndexer) Run(verbose bool) {
	if verbose {
		defer log.Printf("tile indexer done")
	}
	defer close(p.Out)

	t0 := time.Now()
	isInit := true
	for geoReq := range p.In {
		select {
		case <-p.Context.Done():
			p.sendError(fmt.Errorf("tile indexer: context has been cancel: %v", p.Context.Err()))
			return
		case err := <-p.Error:
			p.sendError(err)
			return
		default:
			if len(strings.TrimSpace(geoReq.Collection)) == 0 {
				p.Out <- &GeoTileGranule{ConfigPayLoad: ConfigPayLoad{NameSpaces: []string{utils.EmptyTileNS}, ScaleParams: geoReq.ScaleParams, Palette: geoReq.Palette}, Path: "NULL", NameSpace: utils.EmptyTileNS, RasterType: "Byte", TimeStamp: 0, BBox: geoReq.BBox, Height: geoReq.Height, Width: geoReq.Width, OffX: geoReq.OffX, OffY: geoReq.OffY, CRS: geoReq.CRS}
				return
			}

			var wg sync.WaitGroup
			var url string

			for key, axis := range geoReq.Axes {
				if key == "time" {
					if len(axis.InValues) > 0 {
						var minVal, maxVal float64
						for i, val := range axis.InValues {
							if i == 0 {
								minVal = val
								maxVal = val
							} else {
								if val < minVal {
									minVal = val
								} else if val > maxVal {
									maxVal = val
								}
							}
						}

						minTime := time.Unix(int64(minVal), 0).UTC()
						geoReq.StartTime = &minTime

						maxTime := time.Unix(int64(maxVal), 0).UTC()
						if maxVal > minVal {
							geoReq.EndTime = &maxTime
						}
					} else {
						if axis.Start != nil {
							minTime := time.Unix(int64(*axis.Start), 0).UTC()
							geoReq.StartTime = &minTime
						}

						if axis.End != nil {
							maxTime := time.Unix(int64(*axis.End), 0).UTC()
							geoReq.EndTime = &maxTime
						}
					}
					break
				}
			}

			if len(geoReq.NameSpaces) == 0 {
				geoReq.NameSpaces = append(geoReq.NameSpaces, "")
			}

			nameSpaces := strings.Join(geoReq.NameSpaces, ",")

			isEmptyTile := false
			if len(geoReq.NameSpaces) > 0 && geoReq.NameSpaces[0] == utils.EmptyTileNS {
				isEmptyTile = true
				nameSpaces = ""
			}
			var bboxWkt string
			if geoReq.MasQueryHint != "non_spatial" {
				bboxWkt = BBox2WKT(geoReq.BBox)
			}
			url = p.getIndexerURL(geoReq, nameSpaces, bboxWkt, geoReq.CRS)
			if isInit {
				if geoReq.MetricsCollector != nil {
					defer func() { geoReq.MetricsCollector.Info.Indexer.Duration += time.Since(t0) }()

					origBboxWkt := bboxWkt
					origURL := url
					if len(geoReq.MetricsCollector.Info.Indexer.URL.RawURL) == 0 {
						if len(geoReq.OrigBBox) > 0 {
							origBboxWkt = BBox2WKT(geoReq.OrigBBox)
							origURL = p.getIndexerURL(geoReq, nameSpaces, origBboxWkt, geoReq.CRS)
						}
						geoReq.MetricsCollector.Info.Indexer.URL.RawURL = origURL
					}

					if len(geoReq.MetricsCollector.Info.Indexer.Geometry) == 0 {
						geoReq.MetricsCollector.Info.Indexer.Geometry = origBboxWkt
					}

					if len(geoReq.MetricsCollector.Info.Indexer.SRS) == 0 {
						geoReq.MetricsCollector.Info.Indexer.SRS = geoReq.CRS
					}
				}
				isInit = false
			}

			if verbose {
				log.Println(url)
			}

			hasSubDivision := false
			var clippedBBox []float64
			var err error

			if len(geoReq.SpatialExtent) >= 4 {
				clippedBBox, err = utils.GetCanonicalBbox(geoReq.CRS, geoReq.BBox)
				if err == nil {
					clippedBBox[0] = math.Max(clippedBBox[0], geoReq.SpatialExtent[0])
					clippedBBox[1] = math.Max(clippedBBox[1], geoReq.SpatialExtent[1])

					clippedBBox[2] = math.Min(clippedBBox[2], geoReq.SpatialExtent[2])
					clippedBBox[3] = math.Min(clippedBBox[3], geoReq.SpatialExtent[3])

					if clippedBBox[2] < clippedBBox[0] || clippedBBox[3] < clippedBBox[1] {
						p.Out <- &GeoTileGranule{ConfigPayLoad: ConfigPayLoad{NameSpaces: []string{utils.EmptyTileNS}, ScaleParams: geoReq.ScaleParams, Palette: geoReq.Palette}, Path: "NULL", NameSpace: utils.EmptyTileNS, RasterType: "Byte", TimeStamp: 0, BBox: geoReq.BBox, Height: geoReq.Height, Width: geoReq.Width, OffX: geoReq.OffX, OffY: geoReq.OffY, CRS: geoReq.CRS}
						if verbose {
							log.Printf("Indexer error: invalid bbox: %v", clippedBBox)
						}
						return
					}
					hasSubDivision = true
				} else if verbose {
					log.Printf("Indexer sub-division error: %v", err)
				}
			}

			if hasSubDivision {
				resWidth := 256
				resHeight := 256
				maxXTileSize := int(float64(resWidth) * geoReq.IndexTileXSize)
				if maxXTileSize <= 0 {
					maxXTileSize = resWidth
				}

				maxYTileSize := int(float64(resHeight) * geoReq.IndexTileYSize)
				if maxYTileSize <= 0 {
					maxYTileSize = resHeight
				}

				xRes := (clippedBBox[2] - clippedBBox[0]) / float64(resWidth)
				yRes := (clippedBBox[3] - clippedBBox[1]) / float64(resHeight)
				reqRes := math.Max(xRes, yRes)
				if geoReq.QueryLimit <= 0 && reqRes > geoReq.IndexResLimit {
					cLimiter := NewConcLimiter(DefaultMASConcQuery)
					var urls []string
					for y := 0; y < resHeight; y += maxYTileSize {
						for x := 0; x < resWidth; x += maxXTileSize {
							yMin := clippedBBox[1] + float64(y)*yRes
							yMax := math.Min(clippedBBox[1]+float64(y+maxYTileSize)*yRes, clippedBBox[3])
							xMin := clippedBBox[0] + float64(x)*xRes
							xMax := math.Min(clippedBBox[0]+float64(x+maxXTileSize)*xRes, clippedBBox[2])

							bbox := []float64{xMin, yMin, xMax, yMax}
							bboxWkt = BBox2WKT(bbox)
							url = p.getIndexerURL(geoReq, nameSpaces, bboxWkt, "EPSG:3857")
							urls = append(urls, url)
						}
					}
					rand.Shuffle(len(urls), func(i, j int) { urls[i], urls[j] = urls[j], urls[i] })
					for _, url := range urls {
						wg.Add(1)
						cLimiter.Increase()
						go p.URLIndexGet(p.Context, url, geoReq, p.Error, p.Out, &wg, isEmptyTile, cLimiter, verbose)
					}

				} else {
					hasSubDivision = false
				}
			}

			if !hasSubDivision {
				wg.Add(1)
				go p.URLIndexGet(p.Context, url, geoReq, p.Error, p.Out, &wg, isEmptyTile, nil, verbose)
			}

			if geoReq.Mask != nil {
				maskCollection := geoReq.Mask.DataSource
				if len(maskCollection) == 0 {
					maskCollection = geoReq.Collection
				}

				if maskCollection != geoReq.Collection || geoReq.Mask.ID != nameSpaces {
					if geoReq.EndTime == nil {
						url = strings.Replace(fmt.Sprintf("http://%s%s?intersects&metadata=gdal&time=%s&srs=%s&wkt=%s&namespace=%s&nseg=%d&limit=%d", p.APIAddress, maskCollection, geoReq.StartTime.Format(ISOFormat), geoReq.CRS, bboxWkt, geoReq.Mask.ID, geoReq.PolygonSegments, geoReq.QueryLimit), " ", "%20", -1)
					} else {
						url = strings.Replace(fmt.Sprintf("http://%s%s?intersects&metadata=gdal&time=%s&until=%s&srs=%s&wkt=%s&namespace=%s&nseg=%d&limit=%d", p.APIAddress, maskCollection, geoReq.StartTime.Format(ISOFormat), geoReq.EndTime.Format(ISOFormat), geoReq.CRS, bboxWkt, geoReq.Mask.ID, geoReq.PolygonSegments, geoReq.QueryLimit), " ", "%20", -1)
					}
					if verbose {
						log.Println(url)
					}

					wg.Add(1)
					go p.URLIndexGet(p.Context, url, geoReq, p.Error, p.Out, &wg, isEmptyTile, nil, verbose)
				}
			}

			wg.Wait()
		}
	}
}

func (p *TileIndexer) getIndexerURL(geoReq *GeoTileRequest, nameSpaces string, bboxWkt string, crs string) string {
	var url string
	if geoReq.EndTime == nil {
		url = strings.Replace(fmt.Sprintf("http://%s%s?intersects&metadata=gdal&time=%s&srs=%s&wkt=%s&namespace=%s&nseg=%d&limit=%d", p.APIAddress, geoReq.Collection, geoReq.StartTime.Format(ISOFormat), crs, bboxWkt, nameSpaces, geoReq.PolygonSegments, geoReq.QueryLimit), " ", "%20", -1)
	} else {
		url = strings.Replace(fmt.Sprintf("http://%s%s?intersects&metadata=gdal&time=%s&until=%s&srs=%s&wkt=%s&namespace=%s&nseg=%d&limit=%d", p.APIAddress, geoReq.Collection, geoReq.StartTime.Format(ISOFormat), geoReq.EndTime.Format(ISOFormat), crs, bboxWkt, nameSpaces, geoReq.PolygonSegments, geoReq.QueryLimit), " ", "%20", -1)
	}

	return url
}

func (p *TileIndexer) URLIndexGet(ctx context.Context, url string, geoReq *GeoTileRequest, errChan chan error, out chan *GeoTileGranule, wg *sync.WaitGroup, isEmptyTile bool, cLimiter *ConcLimiter, verbose bool) {
	defer wg.Done()
	if cLimiter != nil {
		defer cLimiter.Decrease()
	}

	resp, err := http.Get(url)
	if err != nil {
		p.sendError(fmt.Errorf("GET request to %s failed. Error: %v", url, err))
		out <- &GeoTileGranule{ConfigPayLoad: ConfigPayLoad{NameSpaces: []string{utils.EmptyTileNS}, ScaleParams: geoReq.ScaleParams, Palette: geoReq.Palette}, Path: "NULL", NameSpace: utils.EmptyTileNS, RasterType: "Byte", TimeStamp: 0, BBox: geoReq.BBox, Height: geoReq.Height, Width: geoReq.Width, OffX: geoReq.OffX, OffY: geoReq.OffY, CRS: geoReq.CRS}
		return
	}
	t0 := time.Now()
	body, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if geoReq.MetricsCollector != nil {
		geoReq.MetricsCollector.Info.Indexer.Duration += time.Since(t0)
	}
	if err != nil {
		p.sendError(fmt.Errorf("Error parsing response body from %s. Error: %v", url, err))
		out <- &GeoTileGranule{ConfigPayLoad: ConfigPayLoad{NameSpaces: []string{utils.EmptyTileNS}, ScaleParams: geoReq.ScaleParams, Palette: geoReq.Palette}, Path: "NULL", NameSpace: utils.EmptyTileNS, RasterType: "Byte", TimeStamp: 0, BBox: geoReq.BBox, Height: geoReq.Height, Width: geoReq.Width, OffX: geoReq.OffX, OffY: geoReq.OffY, CRS: geoReq.CRS}
		return
	}
	var metadata MetadataResponse
	err = json.Unmarshal(body, &metadata)
	if err != nil {
		p.sendError(fmt.Errorf("Problem parsing JSON response from %s. Error: %v", url, err))
		out <- &GeoTileGranule{ConfigPayLoad: ConfigPayLoad{NameSpaces: []string{utils.EmptyTileNS}, ScaleParams: geoReq.ScaleParams, Palette: geoReq.Palette}, Path: "NULL", NameSpace: utils.EmptyTileNS, RasterType: "Byte", TimeStamp: 0, BBox: geoReq.BBox, Height: geoReq.Height, Width: geoReq.Width, OffX: geoReq.OffX, OffY: geoReq.OffY, CRS: geoReq.CRS}
		return
	}

	if geoReq.MetricsCollector != nil {
		geoReq.MetricsCollector.Info.Indexer.NumFiles += len(metadata.GDALDatasets)
	}
	if verbose {
		log.Printf("tile indexer: %d files", len(metadata.GDALDatasets))
	}

	switch len(metadata.GDALDatasets) {
	case 0:
		if len(metadata.Error) > 0 {
			log.Printf("Indexer returned error: %v", string(body))
		}
		out <- &GeoTileGranule{ConfigPayLoad: ConfigPayLoad{NameSpaces: []string{utils.EmptyTileNS}, ScaleParams: geoReq.ScaleParams, Palette: geoReq.Palette}, Path: "NULL", NameSpace: utils.EmptyTileNS, RasterType: "Byte", TimeStamp: 0, BBox: geoReq.BBox, Height: geoReq.Height, Width: geoReq.Width, OffX: geoReq.OffX, OffY: geoReq.OffY, CRS: geoReq.CRS}
	default:
		axisParamsLookup := make(map[string]map[float64]struct{})
		for _, ds := range metadata.GDALDatasets {
			if len(ds.Axes) == 0 {
				ds.Axes = append(ds.Axes, &DatasetAxis{Name: "time", Strides: []int{1}, Grid: "default"})
			}

			isOutRange := false
			for ia, axis := range ds.Axes {
				tileAxis, found := geoReq.Axes[axis.Name]
				if found {
					if axis.Name == "time" && ((tileAxis.Start != nil && tileAxis.End == nil) || len(tileAxis.InValues) > 0 || len(tileAxis.IdxSelectors) > 0) {
						axis.Grid = "enum"
						for _, t := range ds.TimeStamps {
							axis.Params = append(axis.Params, float64(t.Unix()))
						}
						ds.Axes[ia] = axis
					}

					axis.Order = tileAxis.Order
					axis.Aggregate = tileAxis.Aggregate

					var outRange bool
					var selErr error
					if len(tileAxis.IdxSelectors) > 0 {
						if _, found := axisParamsLookup[axis.Name]; !found {
							axisParamsLookup[axis.Name] = make(map[float64]struct{})
							for _, val := range axis.Params {
								axisParamsLookup[axis.Name][val] = struct{}{}
							}
						} else {
							for _, val := range axis.Params {
								if _, found := axisParamsLookup[axis.Name][val]; !found {
									p.sendError(fmt.Errorf("index-based selection only supports homogeneous axis across files"))
									return
								}
							}
						}
						outRange, selErr = doSelectionByIndices(axis, tileAxis)
					} else {
						outRange, selErr = doSelectionByRange(axis, tileAxis, geoReq, ds)
					}

					if selErr != nil {
						p.sendError(selErr)
						return
					}
					isOutRange = outRange
				} else {
					if geoReq.AxisMapping == 0 {
						if axis.Grid == "enum" {
							if len(axis.Params) == 0 {
								p.sendError(fmt.Errorf("Indexer error: empty params for 'enum' grid: %v", axis.Name))
								return
							}
							axis.IntersectionIdx = append(axis.IntersectionIdx, 0)
							axis.Order = 1
							axis.Aggregate = 1
							axis.IntersectionValues = append(axis.IntersectionValues, axis.Params[0])
						} else if axis.Grid == "default" {
							axis.IntersectionIdx = append(axis.IntersectionIdx, 0)
							axis.Order = 1
							axis.Aggregate = 1
							axis.IntersectionValues = append(axis.IntersectionValues, float64(ds.TimeStamps[0].Unix()))
						} else {
							p.sendError(fmt.Errorf("Indexer error: unknown axis grid type: %v", axis.Grid))
							return
						}
					} else {
						if axis.Grid == "enum" {
							if len(axis.Params) == 0 {
								p.sendError(fmt.Errorf("Indexer error: empty params for 'enum' grid: %v", axis.Name))
								return
							}
							for iv, val := range axis.Params {
								axis.IntersectionIdx = append(axis.IntersectionIdx, iv)
								axis.IntersectionValues = append(axis.IntersectionValues, val)
							}
						} else if axis.Grid == "default" {
							for it, t := range ds.TimeStamps {
								axis.IntersectionIdx = append(axis.IntersectionIdx, it)
								axis.IntersectionValues = append(axis.IntersectionValues, float64(t.Unix()))
							}
						} else {
							p.sendError(fmt.Errorf("Indexer error: unknown axis grid type: %v", axis.Grid))
							return
						}

					}
				}

				for i := range axis.IntersectionIdx {
					axis.IntersectionIdx[i] *= axis.Strides[0]
				}
			}

			if isOutRange {
				ds.IsOutRange = true
				continue
			}

		}

		bandNameSpaces := make(map[string]map[string]float64)
		var granList []*GeoTileGranule
		for _, ds := range metadata.GDALDatasets {
			if ds.IsOutRange {
				continue
			}

			axisIdxCnt := make([]int, len(ds.Axes))

			dsNameSpace := ds.NameSpace
			if isEmptyTile {
				dsNameSpace = utils.EmptyTileNS
			}

			for axisIdxCnt[0] < len(ds.Axes[0].IntersectionIdx) {
				bandIdx := 1
				aggTimeStamp := 0.0
				bandTimeStamp := 0.0

				namespace := dsNameSpace
				isFirst := true
				hasNonAgg := false
				for i := 0; i < len(axisIdxCnt); i++ {
					bandIdx += ds.Axes[i].IntersectionIdx[axisIdxCnt[i]]

					iTimeStamp := axisIdxCnt[i]
					bandTimeStamp += ds.Axes[i].IntersectionValues[iTimeStamp]

					if ds.Axes[i].Order != 0 {
						iTimeStamp = len(ds.Axes[i].IntersectionIdx) - axisIdxCnt[i] - 1
					}
					aggTimeStamp += ds.Axes[i].IntersectionValues[iTimeStamp]

					if ds.Axes[i].Aggregate == 0 {
						if isFirst {
							namespace += "#"
							isFirst = false
						} else {
							namespace += ","
						}

						var readableNs string
						if ds.Axes[i].Name == "time" {
							ts := time.Unix(int64(ds.Axes[i].IntersectionValues[axisIdxCnt[i]]), 0).UTC()
							readableNs = fmt.Sprintf("%v", ts.Format(ISOFormat))
						} else {
							readableNs = fmt.Sprintf("%v", ds.Axes[i].IntersectionValues[axisIdxCnt[i]])
						}
						namespace += fmt.Sprintf("%s=%v", ds.Axes[i].Name, readableNs)
						hasNonAgg = true
					}
				}

				bandFound := false
				if hasNonAgg {
					_, found := bandNameSpaces[dsNameSpace]
					if !found {
						bandNameSpaces[dsNameSpace] = make(map[string]float64)
					}

					_, bFound := bandNameSpaces[dsNameSpace][namespace]
					if !bFound {
						bandNameSpaces[dsNameSpace][namespace] = bandTimeStamp
					}
					bandFound = bFound
				}

				if !isEmptyTile || (isEmptyTile && !bandFound) {
					gran := &GeoTileGranule{ConfigPayLoad: geoReq.ConfigPayLoad, RawPath: ds.RawPath, Path: ds.DSName, NameSpace: namespace, VarNameSpace: ds.NameSpace, RasterType: ds.ArrayType, TimeStamp: float64(aggTimeStamp), BandIdx: bandIdx, Polygon: ds.Polygon, BBox: geoReq.BBox, Height: geoReq.Height, Width: geoReq.Width, CRS: geoReq.CRS, SrcSRS: ds.SRS, SrcGeoTransform: ds.GeoTransform, GeoLocation: ds.GeoLocation, ClipFeature: geoReq.ClipFeature}
					if isEmptyTile {
						gran.Path = "NULL"
						gran.RasterType = "Byte"
						gran.Height = 1
						gran.Width = 1
					}
					granList = append(granList, gran)
				}

				//log.Printf("    %v, %v,%v,%v,%v   %v", axisIdxCnt, bandIdx, aggTimeStamp, bandTimeStamp, namespace, len(ds.TimeStamps))

				ia := len(ds.Axes) - 1
				axisIdxCnt[ia]++
				for ; ia > 0 && axisIdxCnt[ia] >= len(ds.Axes[ia].IntersectionIdx); ia-- {
					axisIdxCnt[ia] = 0
					axisIdxCnt[ia-1]++
				}
			}

		}

		type _BandNs struct {
			Name string
			Val  float64
		}

		var sortedNameSpaces []string
		hasNewNs := false
		for _, ns := range geoReq.NameSpaces {
			bands, found := bandNameSpaces[ns]
			if found {
				bandNsList := make([]*_BandNs, len(bands))
				ib := 0
				for bns, val := range bands {
					bandNsList[ib] = &_BandNs{Name: bns, Val: val}
					ib++
				}

				sort.Slice(bandNsList, func(i, j int) bool { return bandNsList[i].Val <= bandNsList[j].Val })

				for _, bns := range bandNsList {
					sortedNameSpaces = append(sortedNameSpaces, bns.Name)
				}

				hasNewNs = true
			} else {
				sortedNameSpaces = append(sortedNameSpaces, ns)
			}
		}

		var newConfigPayLoad ConfigPayLoad
		if hasNewNs {
			newConfigPayLoad = geoReq.ConfigPayLoad
			newConfigPayLoad.NameSpaces = sortedNameSpaces
		}
		//log.Printf("%#v, %#v", geoReq.ConfigPayLoad, newConfigPayLoad)

		if geoReq.MetricsCollector != nil {
			geoReq.MetricsCollector.Info.Indexer.NumGranules += len(granList)
		}
		if verbose {
			log.Printf("tile indexer: %d granules", len(granList))
		}

		for _, gran := range granList {
			if hasNewNs {
				gran.ConfigPayLoad = newConfigPayLoad
			}
			if p.checkCancellation() {
				return
			}
			out <- gran
		}
	}
}

func doSelectionByIndices(axis *DatasetAxis, tileAxis *GeoTileAxis) (bool, error) {
	if axis.Grid != "enum" {
		return false, fmt.Errorf("grid type must be 'enum' for index-based selections")
	}

	idxLookup := make(map[int]struct{})
	for _, sel := range tileAxis.IdxSelectors {
		if sel.IsAll {
			axis.IntersectionIdx = make([]int, len(axis.Params))
			axis.IntersectionValues = make([]float64, len(axis.Params))
			for ip, val := range axis.Params {
				axis.IntersectionIdx[ip] = ip
				axis.IntersectionValues[ip] = val
			}
			return false, nil
		}

		if !sel.IsRange {
			if sel.Start == nil {
				return false, fmt.Errorf("starting index is null")
			}

			idx := *sel.Start
			if idx < 0 || idx > len(axis.Params)-1 {
				return true, nil
			}

			if _, found := idxLookup[idx]; found {
				continue
			}

			idxLookup[idx] = struct{}{}
			axis.IntersectionIdx = append(axis.IntersectionIdx, idx)
			axis.IntersectionValues = append(axis.IntersectionValues, axis.Params[idx])
			continue
		}

		idxStart := 0
		if sel.Start != nil {
			idxStart = *sel.Start
		}
		idxEnd := len(axis.Params) - 1
		if sel.End != nil {
			idxEnd = *sel.End
		}

		if idxEnd > len(axis.Params)-1 {
			return true, nil
		}

		if idxStart > idxEnd {
			return false, fmt.Errorf("starting index must be lower or equal to ending index")
		}

		step := 1
		if sel.Step != nil {
			step = *sel.Step
		}

		if step < 1 {
			return false, fmt.Errorf("indexing step must be greater or equal to 1")
		}

		for idx := idxStart; idx <= idxEnd; idx += step {
			if _, found := idxLookup[idx]; found {
				continue
			}
			axis.IntersectionIdx = append(axis.IntersectionIdx, idx)
			axis.IntersectionValues = append(axis.IntersectionValues, axis.Params[idx])
		}
	}

	type ArgSort struct {
		SortIdx int
		Val     int
	}

	argSortIdx := make([]*ArgSort, len(axis.IntersectionIdx))
	for i, idx := range axis.IntersectionIdx {
		argSortIdx[i] = &ArgSort{SortIdx: i, Val: idx}
	}
	sort.Slice(argSortIdx, func(i, j int) bool { return argSortIdx[i].Val <= argSortIdx[j].Val })

	newIntersectionIdx := make([]int, len(axis.IntersectionIdx))
	for i, arg := range argSortIdx {
		newIntersectionIdx[i] = arg.Val
	}
	axis.IntersectionIdx = newIntersectionIdx

	newIntersectionValues := make([]float64, len(axis.IntersectionValues))
	for i, arg := range argSortIdx {
		newIntersectionValues[i] = axis.IntersectionValues[arg.SortIdx]
	}
	axis.IntersectionValues = newIntersectionValues

	return false, nil
}

func doSelectionByRange(axis *DatasetAxis, tileAxis *GeoTileAxis, geoReq *GeoTileRequest, ds *GDALDataset) (bool, error) {
	if axis.Grid == "enum" {
		if len(axis.Params) > 0 {
			if len(tileAxis.InValues) > 0 || (tileAxis.Start != nil && tileAxis.End == nil) {
				if len(tileAxis.InValues) == 0 {
					tileAxis.InValues = append(tileAxis.InValues, *tileAxis.Start)
				}

				isMonotonic := true
				minVal := axis.Params[0]
				maxVal := axis.Params[len(axis.Params)-1]
				for iv, val := range axis.Params {
					if val < minVal {
						minVal = val
					}

					if val > maxVal {
						maxVal = val
					}

					if iv > 0 && isMonotonic && val < axis.Params[iv-1] {
						isMonotonic = false
					}
				}

				var inValues []float64
				for _, val := range tileAxis.InValues {
					if minVal-val > 1e-06 || val-maxVal > 1e-06 {
						continue
					}
					inValues = append(inValues, val)
				}

				if len(inValues) == 0 {
					return true, nil
				}

				if isMonotonic {
					iVal := 0
					var startVal float64
					var nVals int

					startVal = inValues[0]
					nVals = len(inValues)
					if len(inValues) > 1 {
						sort.Slice(inValues, func(i, j int) bool { return inValues[i] <= inValues[j] })
					}

					for iv, val := range axis.Params {
						valFound := false
						if iv < len(axis.Params)-1 {
							valFound = val >= startVal
						} else {
							valFound = startVal-val <= 1e-6
						}
						if valFound {
							axisIdx := 0
							if iv >= 1 && math.Abs(startVal-axis.Params[iv-1]) <= math.Abs(startVal-val) {
								axisIdx = iv - 1
							} else {
								axisIdx = iv
							}

							axis.IntersectionIdx = append(axis.IntersectionIdx, axisIdx)
							axis.IntersectionValues = append(axis.IntersectionValues, axis.Params[axisIdx])

							iVal++
							if iVal >= nVals {
								break
							}

							startVal = inValues[iVal]
						}
					}
				} else {
					for _, val := range inValues {
						minDiff := math.MaxFloat64
						minIdx := 0
						for iv, pv := range axis.Params {
							diff := math.Abs(pv - val)
							if diff < minDiff {
								minDiff = diff
								minIdx = iv
							}
						}

						axis.IntersectionIdx = append(axis.IntersectionIdx, minIdx)
						axis.IntersectionValues = append(axis.IntersectionValues, axis.Params[minIdx])
					}
				}

			} else if tileAxis.Start != nil && tileAxis.End != nil {
				startVal := *tileAxis.Start
				endVal := *tileAxis.End
				if endVal < axis.Params[0] || startVal > axis.Params[len(axis.Params)-1] {
					return true, nil
				}
				for iv, val := range axis.Params {
					if val >= startVal && val < endVal {
						axis.IntersectionIdx = append(axis.IntersectionIdx, iv)
						axis.IntersectionValues = append(axis.IntersectionValues, axis.Params[iv])
					}
				}

			}

		} else {
			return false, fmt.Errorf("Indexer error: empty params for 'enum' grid: %v", axis.Name)
		}
	} else if axis.Grid == "default" {
		for it, t := range ds.TimeStamps {
			if t.Equal(*geoReq.StartTime) || geoReq.EndTime != nil && (t.Equal(*geoReq.EndTime) || t.After(*geoReq.StartTime) && t.Before(*geoReq.EndTime)) {
				axis.IntersectionIdx = append(axis.IntersectionIdx, it)
				axis.IntersectionValues = append(axis.IntersectionValues, float64(ds.TimeStamps[it].Unix()))
			}
		}

		if len(axis.IntersectionIdx) == 0 {
			return true, nil
		}
	} else {
		return false, fmt.Errorf("Indexer error: unknown axis grid type: %v", axis.Grid)
	}

	return false, nil
}

func (p *TileIndexer) sendError(err error) {
	select {
	case p.Error <- err:
	default:
	}
}

func (p *TileIndexer) checkCancellation() bool {
	select {
	case <-p.Context.Done():
		p.sendError(fmt.Errorf("tile indexer: context has been cancel: %v", p.Context.Err()))
		return true
	case err := <-p.Error:
		p.sendError(err)
		return true
	default:
		return false
	}
}
