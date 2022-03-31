package gdalprocess

/*
#cgo pkg-config: gdal

#include "warper.hxx"
#include "gdal.h"
*/
import "C"

/*
// This is a reference implementation of warp.
// We leave this code here for debugging and comparsion purposes.
int warp_operation(GDALDatasetH hSrcDS, GDALDatasetH hDstDS, int band)
{
	const char *srcProjRef;
	int err;
	GDALWarpOptions *psWOptions;

	psWOptions = GDALCreateWarpOptions();
	psWOptions->nBandCount = 1;
	psWOptions->panSrcBands = (int *) CPLMalloc(sizeof(int) * 1);
	psWOptions->panSrcBands[0] = band;
	psWOptions->panDstBands = (int *) CPLMalloc(sizeof(int) * 1);
	psWOptions->panDstBands[0] = 1;

	srcProjRef = GDALGetProjectionRef(hSrcDS);
	if(strlen(srcProjRef) == 0) {
		srcProjRef = "GEOGCS[\"WGS 84\",DATUM[\"WGS_1984\",SPHEROID[\"WGS 84\",6378137,298.257223563,AUTHORITY[\"EPSG\",\"7030\"]],TOWGS84[0,0,0,0,0,0,0],AUTHORITY[\"EPSG\",\"6326\"]],PRIMEM[\"Greenwich\",0,AUTHORITY[\"EPSG\",\"8901\"]],UNIT[\"degree\",0.0174532925199433,AUTHORITY[\"EPSG\",\"9108\"]],AUTHORITY[\"EPSG\",\"4326\"]]\",\"proj4\":\"+proj=longlat +ellps=WGS84 +towgs84=0,0,0,0,0,0,0 +no_defs \"";
	}

	err = GDALReprojectImage(hSrcDS, srcProjRef, hDstDS, GDALGetProjectionRef(hDstDS), GRA_NearestNeighbour, 0.0, 0.0, NULL, NULL, psWOptions);
	GDALDestroyWarpOptions(psWOptions);

	return err;
}
*/

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"reflect"
	"syscall"
	"time"
	"unsafe"

	geo "github.com/nci/geometry"
	pb "github.com/nci/gsky/worker/gdalservice"
)

const SizeofUint16 = 2
const SizeofInt16 = 2
const SizeofFloat32 = 4

var GDALTypes = map[C.GDALDataType]string{0: "Unkown", 1: "Byte", 2: "UInt16", 3: "Int16",
	4: "UInt32", 5: "Int32", 6: "Float32", 7: "Float64",
	8: "CInt16", 9: "CInt32", 10: "CFloat32", 11: "CFloat64",
	12: "TypeCount"}

func ComputeReprojectExtent(in *pb.GeoRPCGranule) *pb.Result {
	srcFileC := C.CString(in.Path)
	defer C.free(unsafe.Pointer(srcFileC))

	hSrcDS := C.GDALOpenEx(srcFileC, C.GDAL_OF_READONLY|C.GDAL_OF_VERBOSE_ERROR, nil, nil, nil)
	if hSrcDS == nil {
		return &pb.Result{Error: fmt.Sprintf("Failed to open existing dataset: %v", in.Path)}
	}
	defer C.GDALClose(hSrcDS)

	dstProjRefC := C.CString(in.DstSRS)
	defer C.free(unsafe.Pointer(dstProjRefC))

	hTransformArg := C.GDALCreateGenImgProjTransformer(hSrcDS, nil, nil, dstProjRefC, C.int(0), C.double(0), C.int(0))
	if hTransformArg == nil {
		return &pb.Result{Error: fmt.Sprintf("GDALCreateGenImgProjTransformer() failed")}
	}
	defer C.GDALDestroyGenImgProjTransformer(hTransformArg)

	psInfo := (*C.GDALTransformerInfo)(hTransformArg)

	var padfGeoTransformOut [6]C.double
	var pnPixels, pnLines C.int
	gerr := C.GDALSuggestedWarpOutput(hSrcDS, psInfo.pfnTransform, hTransformArg, &padfGeoTransformOut[0], &pnPixels, &pnLines)
	if gerr != 0 {
		return &pb.Result{Error: fmt.Sprintf("GDALSuggestedWarpOutput() failed")}
	}

	xRes := float64(padfGeoTransformOut[1])
	yRes := float64(math.Abs(float64(padfGeoTransformOut[5])))

	xMin := in.DstGeot[0]
	yMin := in.DstGeot[1]
	xMax := in.DstGeot[2]
	yMax := in.DstGeot[3]

	nPixels := int((xMax - xMin + xRes/2.0) / xRes)
	nLines := int((yMax - yMin + yRes/2.0) / yRes)

	out := make([]int, 2)
	out[0] = nPixels
	out[1] = nLines

	header := *(*reflect.SliceHeader)(unsafe.Pointer(&out))
	intSize := int(unsafe.Sizeof(int(0)))
	header.Len *= intSize
	header.Cap *= intSize
	dBytes := *(*[]uint8)(unsafe.Pointer(&header))

	dBytesCopy := make([]uint8, len(dBytes))
	for i := 0; i < len(dBytes); i++ {
		dBytesCopy[i] = dBytes[i]
	}
	return &pb.Result{Raster: &pb.Raster{Data: dBytesCopy, NoData: 0, RasterType: "Int"}, Error: "OK"}
}

func WarpRaster(in *pb.GeoRPCGranule) *pb.Result {

	filePathC := C.CString(in.Path)
	defer C.free(unsafe.Pointer(filePathC))

	var dstProjRefC *C.char
	if len(in.DstSRS) > 0 {
		dstProjRefC = C.CString(in.DstSRS)
		defer C.free(unsafe.Pointer(dstProjRefC))
	} else {
		dstProjRefC = nil
	}

	dump := func(msg interface{}) string {
		log.Println(
			"warp", in.Path,
			"band", in.Bands[0],
			"width", in.Width,
			"height", in.Height,
			"geotransform", in.DstGeot,
			"srs", in.DstSRS,
			"error", msg,
		)
		return fmt.Sprintf("%v", msg)
	}

	var geoLocOpts []*C.char
	var pGeoLoc **C.char
	if len(in.GeoLocOpts) > 0 {
		for _, opt := range in.GeoLocOpts {
			geoLocOpts = append(geoLocOpts, C.CString(opt))
		}

		for _, opt := range geoLocOpts {
			defer C.free(unsafe.Pointer(opt))
		}
		geoLocOpts = append(geoLocOpts, nil)

		pGeoLoc = &geoLocOpts[0]
	} else {
		pGeoLoc = nil
	}

	var srcProjRefC *C.char
	if len(in.SrcSRS) > 0 {
		srcProjRefC = C.CString(in.SrcSRS)
		defer C.free(unsafe.Pointer(srcProjRefC))
	} else {
		srcProjRefC = nil
	}

	var pSrcGeot *C.double
	if len(in.SrcGeot) > 0 {
		pSrcGeot = (*C.double)(&in.SrcGeot[0])
	} else {
		pSrcGeot = nil
	}

	var dstBboxC [4]C.int
	var dstBufSize C.int
	var dstBufC unsafe.Pointer
	var noData float64
	var dType C.GDALDataType
	var bytesReadC C.size_t

	var resUsage0, resUsage1 syscall.Rusage
	syscall.Getrusage(syscall.RUSAGE_SELF, &resUsage0)
	cErr := C.warp_operation_fast(filePathC, srcProjRefC, pSrcGeot, pGeoLoc, dstProjRefC, (*C.double)(&in.DstGeot[0]), C.int(in.Width), C.int(in.Height), C.int(in.Bands[0]), C.int(in.SRSCf), (*unsafe.Pointer)(&dstBufC), (*C.int)(&dstBufSize), (*C.int)(&dstBboxC[0]), (*C.double)(&noData), (*C.GDALDataType)(&dType), &bytesReadC)
	syscall.Getrusage(syscall.RUSAGE_SELF, &resUsage1)

	metrics := &pb.WorkerMetrics{
		BytesRead: int64(bytesReadC),
		UserTime:  resUsage1.Utime.Nano() - resUsage0.Utime.Nano(),
		SysTime:   resUsage1.Stime.Nano() - resUsage0.Stime.Nano(),
	}

	if cErr != 0 {
		return &pb.Result{Error: dump(fmt.Sprintf("warp_operation() fail: %v", int(cErr))), Metrics: metrics}
	}

	dstBbox := make([]int32, len(dstBboxC))
	for i, v := range dstBboxC {
		dstBbox[i] = int32(v)
	}

	bboxCanvas := C.GoBytes(dstBufC, dstBufSize)
	C.free(dstBufC)

	var rasterType string
	if C.int(dType) == 100 {
		rasterType = "SignedByte"
	} else {
		rasterType = GDALTypes[dType]
	}

	var geomMaskVals []int32

	// create geometry mask
	if in.Geometry != "" {
		var feat geo.Feature
		err := json.Unmarshal([]byte(in.Geometry), &feat)
		if err != nil {
			return &pb.Result{Error: fmt.Sprintf("Problem unmarshalling geometry %v", in)}
		}

		// get canvas
		maskVals, err := createGeomMask(in.Path, feat)

		if err != nil {
			return &pb.Result{Error: fmt.Sprintf("error creating geom mask")}
		}

		hSrcDS := C.GDALOpen(filePathC, C.GDAL_OF_READONLY)
		if hSrcDS == nil {
			return &pb.Result{Error: fmt.Sprintf("Failed to open dataset %s:", in.Path)}
		}
		defer C.GDALClose(hSrcDS)

		geot := make([]float64, 6)
		C.GDALGetGeoTransform(hSrcDS, (*C.double)(&geot[0]))

		xSize := int(C.GDALGetRasterXSize(hSrcDS))
		ySize := int(C.GDALGetRasterYSize(hSrcDS))

		driverStr := C.CString("GTiff")
		defer C.free(unsafe.Pointer(driverStr))
		hDriver := C.GDALGetDriverByName(driverStr)

		// create in-memory vsimem GDAL dataset. Should be cleaned automatically.
		outFile := fmt.Sprintf("/vsimem/%d.tif", time.Now().Unix())
		outFileC := C.CString(outFile)
		defer C.free(unsafe.Pointer(outFileC))

		var hDs C.GDALDatasetH
		hDs = C.GDALCreate(hDriver, outFileC, C.int(xSize), C.int(ySize), 1, C.GDT_Byte, nil)

		if hDs == nil {
			return &pb.Result{Error: fmt.Sprintf("error creating raster")}
		}

		hBand := C.GDALGetRasterBand(hDs, C.int(1))
		C.GDALSetRasterNoDataValue(hBand, C.double(200))

		gerr := C.CPLErr(0)

		gerr = C.GDALRasterIO(hBand, C.GF_Write, 0, 0, C.int(xSize), C.int(ySize), unsafe.Pointer(&maskVals[0]), C.int(xSize), C.int(ySize), C.GDT_Byte, 0, 0)

		if gerr != 0 {
			C.GDALClose(hDs)
			return &pb.Result{Error: fmt.Sprintf("error writing raster band")}
		}

		// Set projection
		hSRS := C.OSRNewSpatialReference(nil)
		defer C.OSRDestroySpatialReference(hSRS)
		C.OSRImportFromEPSG(hSRS, C.int(4326))
		var projWKT *C.char
		defer C.free(unsafe.Pointer(projWKT))
		C.OSRExportToWkt(hSRS, &projWKT)
		C.GDALSetProjection(hDs, projWKT)
		// Set geotransform
		C.GDALSetGeoTransform(hDs, (*C.double)(&geot[0]))

		// close to flush dataset
		C.GDALClose(hDs)

		var pSrcGeotG *C.double
		if len(in.SrcGeot) > 0 {
			pSrcGeotG = (*C.double)(&in.SrcGeot[0])
		} else {
			pSrcGeotG = nil
		}

		var dstBboxCG [4]C.int
		var dstBufSizeG C.int
		var dstBufCG unsafe.Pointer
		var noDataG float64
		var dTypeG C.GDALDataType
		var bytesReadCG C.size_t

		// warp geometry raster
		cErr := C.warp_operation_fast(outFileC, srcProjRefC, pSrcGeotG, pGeoLoc, dstProjRefC, (*C.double)(&in.DstGeot[0]), C.int(in.Width), C.int(in.Height), C.int(in.Bands[0]), C.int(in.SRSCf), (*unsafe.Pointer)(&dstBufCG), (*C.int)(&dstBufSizeG), (*C.int)(&dstBboxCG[0]), (*C.double)(&noDataG), (*C.GDALDataType)(&dTypeG), &bytesReadCG)
		if cErr != 0 {
			return &pb.Result{Error: fmt.Sprintf("warp_operation() fail: %v", int(cErr))}
		}

		bboxCanvasG := C.GoBytes(dstBufCG, dstBufSizeG)
		C.free(dstBufCG)

		// convert to int32. int32 preffered here since protobuf does not have uint8 to rep single byte ?
		// https://stackoverflow.com/questions/47411248/define-uint8-t-variable-in-protocol-buffers-message-file
		for _, m := range bboxCanvasG {
			val := int32(m)
			geomMaskVals = append(geomMaskVals, val)
		}
	}

	return &pb.Result{Raster: &pb.Raster{Data: bboxCanvas, NoData: noData, RasterType: rasterType, Bbox: dstBbox, Mask: geomMaskVals}, Error: "OK", Metrics: metrics}
}

func createGeomMask(filePath string, feature geo.Feature) ([]uint8, error) {
	filePathC := C.CString(filePath)
	defer C.free(unsafe.Pointer(filePathC))

	geomGeoJSON, err := json.Marshal(feature.Geometry)

	if err != nil {
		return nil, err
	}

	cGeom := C.CString(string(geomGeoJSON))
	defer C.free(unsafe.Pointer(cGeom))
	geom := C.OGR_G_CreateGeometryFromJson(cGeom)

	// assign spatial reference
	selSRS := C.OSRNewSpatialReference(cWGS84WKT)
	defer C.OSRDestroySpatialReference(selSRS)
	C.OGR_G_AssignSpatialReference(geom, selSRS)

	gCopy := C.OGR_G_Buffer(geom, C.double(0.0), C.int(30))
	if C.OGR_G_IsEmpty(gCopy) == C.int(1) {
		gCopy = C.OGR_G_Clone(geom)
	}

	defer C.OGR_G_DestroyGeometry(gCopy)

	hSrcDS := C.GDALOpen(filePathC, C.GDAL_OF_READONLY)
	if hSrcDS == nil {
		log.Fatalf("Failed to open dataset %s:", filePath)
	}
	defer C.GDALClose(hSrcDS)

	if C.GoString(C.GDALGetProjectionRef(hSrcDS)) != "" {
		desSRS := C.OSRNewSpatialReference(C.GDALGetProjectionRef(hSrcDS))
		defer C.OSRDestroySpatialReference(desSRS)
		srcSRS := C.OSRNewSpatialReference(cWGS84WKT)
		defer C.OSRDestroySpatialReference(srcSRS)
		C.OSRSetAxisMappingStrategy(srcSRS, C.OAMS_TRADITIONAL_GIS_ORDER)
		C.OSRSetAxisMappingStrategy(desSRS, C.OAMS_TRADITIONAL_GIS_ORDER)
		trans := C.OCTNewCoordinateTransformation(srcSRS, desSRS)
		C.OGR_G_Transform(gCopy, trans)
		C.OCTDestroyCoordinateTransformation(trans)
	}

	geot := make([]float64, 6)
	C.GDALGetGeoTransform(hSrcDS, (*C.double)(&geot[0]))

	xSize := int(C.GDALGetRasterXSize(hSrcDS))
	ySize := int(C.GDALGetRasterYSize(hSrcDS))

	canvasG := make([]uint8, xSize*ySize)

	// initialize with zeros
	for i := range canvasG {
		canvasG[i] = 0
	}

	memStrC := C.CString(fmt.Sprintf("MEM:::DATAPOINTER=%d,PIXELS=%d,LINES=%d,DATATYPE=Byte", unsafe.Pointer(&canvasG[0]), C.int(xSize), C.int(ySize)))
	defer C.free(unsafe.Pointer(memStrC))
	hMaskDS := C.GDALOpen(memStrC, C.GA_Update)
	if hMaskDS == nil {
		return nil, fmt.Errorf("Couldn't create memory driver")
	}
	defer C.GDALClose(hMaskDS)

	var gdalErr C.CPLErr

	if gdalErr = C.GDALSetProjection(hMaskDS, C.GDALGetProjectionRef(hSrcDS)); gdalErr != 0 {
		return nil, fmt.Errorf("couldn't set a projection in the mem raster %v", gdalErr)
	}

	if gdalErr = C.GDALSetGeoTransform(hMaskDS, (*C.double)(&geot[0])); gdalErr != 0 {
		return nil, fmt.Errorf("couldn't set the geotransform on the destination dataset %v", gdalErr)
	}

	ic := C.OGR_G_Clone(geom)
	defer C.OGR_G_DestroyGeometry(ic)

	geomBurnValue := C.double(255)
	panBandList := []C.int{C.int(1)}
	pahGeomList := []C.OGRGeometryH{ic}

	opts := []*C.char{C.CString("ALL_TOUCHED=TRUE"), nil}
	defer C.free(unsafe.Pointer(opts[0]))

	// rasterize
	if gdalErr = C.GDALRasterizeGeometries(hMaskDS, 1, &panBandList[0], 1, &pahGeomList[0], nil, nil, &geomBurnValue, &opts[0], nil, nil); gdalErr != 0 {
		return nil, fmt.Errorf("Error Rasterizing %v", gdalErr)
	}

	var maskVals []uint8

	for _, m := range canvasG {
		val := uint8(m)
		maskVals = append(maskVals, val)
	}

	return maskVals, nil
}
