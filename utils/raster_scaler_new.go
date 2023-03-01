package utils

import (
	"fmt"
	"math"
)

func scaleNew(r Raster, params ScaleParams) (*ByteRaster, error) {

	steps := 256

	switch t := r.(type) {
	case *SignedByteRaster:
		out := &ByteRaster{NameSpace: t.NameSpace, NoData: t.NoData, Data: make([]uint8, t.Height*t.Width), Width: t.Width, Height: t.Height}
		noData := int8(t.NoData)
		offset := int8(params.Offset)
		clip := int8(params.Clip)

		if params.Clip == 0.0 && params.Offset == 0.0 {
			var minVal, maxVal float32
			for i, value := range t.Data {
				if value == noData {
					continue
				}

				val := float32(value)
				if i == 0 {
					minVal = val
					maxVal = val
				} else {
					if val < minVal {
						minVal = val
					}

					if val > maxVal {
						maxVal = val
					}
				}
			}

			if minVal == maxVal {
				maxVal += 0.1
			}

			offset = int8(minVal)
			clip = int8(maxVal)
		}

		trange := clip - offset

		for i, value := range t.Data {
			if value == noData {
				out.Data[i] = 0xFF
			} else {
				if value > clip {
					value = clip
				}

				out.Data[i] = uint8(math.Floor(float64(((value - clip) / trange * (int8(steps) - 1)))))
			}
		}
		return out, nil

	case *ByteRaster:
		noData := uint8(t.NoData)
		offset := uint8(params.Offset)
		clip := uint8(params.Clip)

		if params.Clip == 0.0 && params.Offset == 0.0 {
			var minVal, maxVal float32
			for i, value := range t.Data {
				if value == noData {
					continue
				}

				val := float32(value)
				if i == 0 {
					minVal = val
					maxVal = val
				} else {
					if val < minVal {
						minVal = val
					}

					if val > maxVal {
						maxVal = val
					}
				}
			}

			if minVal == maxVal {
				maxVal += 0.1
			}

			offset = uint8(minVal)
			clip = uint8(maxVal)

		}

		trange := clip - offset

		for i, value := range t.Data {
			if value == noData {
				t.Data[i] = 0xFF
			} else {

				if value > clip {
					value = clip
				}
				t.Data[i] = uint8(math.Floor(float64(((value - clip) / trange * (uint8(steps) - 1)))))
			}
		}
		return &ByteRaster{t.NameSpace, t.Data, t.Height, t.Width, t.NoData}, nil

	case *Int16Raster:
		out := &ByteRaster{NameSpace: t.NameSpace, NoData: t.NoData, Data: make([]uint8, t.Height*t.Width), Width: t.Width, Height: t.Height}
		noData := int16(t.NoData)
		offset := int16(params.Offset)
		clip := int16(params.Clip)

		if params.Clip == 0.0 && params.Offset == 0.0 {
			var minVal, maxVal float32
			for i, value := range t.Data {
				if value == noData {
					continue
				}

				val := float32(value)
				if i == 0 {
					minVal = val
					maxVal = val
				} else {
					if val < minVal {
						minVal = val
					}

					if val > maxVal {
						maxVal = val
					}
				}
			}

			if minVal == maxVal {
				maxVal += 0.1
			}

			offset = int16(minVal)
			clip = int16(maxVal)
		}

		trange := clip - offset

		for i, value := range t.Data {
			if value == noData {
				out.Data[i] = 0xFF
			} else {

				if value > clip {
					value = clip
				}

				out.Data[i] = uint8(math.Floor(float64(((value - clip) / trange * (int16(steps) - 1)))))
			}
		}
		return out, nil

	case *UInt16Raster:
		out := &ByteRaster{NameSpace: t.NameSpace, NoData: t.NoData, Data: make([]uint8, t.Height*t.Width), Width: t.Width, Height: t.Height}
		noData := uint16(t.NoData)
		offset := uint16(params.Offset)
		clip := uint16(params.Clip)

		if params.Clip == 0.0 && params.Offset == 0.0 {
			var minVal, maxVal float32
			for i, value := range t.Data {
				if value == noData {
					continue
				}

				val := float32(value)
				if i == 0 {
					minVal = val
					maxVal = val
				} else {
					if val < minVal {
						minVal = val
					}

					if val > maxVal {
						maxVal = val
					}
				}
			}

			if minVal == maxVal {
				maxVal += 0.1
			}

			offset = uint16(minVal)
			clip = uint16(maxVal)
		}

		trange := clip - offset

		for i, value := range t.Data {
			if value == noData {
				out.Data[i] = 0xFF
			} else {

				if value > clip {
					value = clip
				}

				out.Data[i] = uint8(math.Floor(float64(((value - clip) / trange * (uint16(steps) - 1)))))
			}
		}
		return out, nil

	case *Float32Raster:
		out := &ByteRaster{NameSpace: t.NameSpace, NoData: t.NoData, Data: make([]uint8, t.Height*t.Width), Width: t.Width, Height: t.Height}
		noData := float32(t.NoData)
		offset := float32(params.Offset)
		clip := float32(params.Clip)

		if params.Clip == 0.0 && params.Offset == 0.0 {
			var minVal, maxVal float32
			for i, value := range t.Data {
				if value == noData {
					continue
				}

				if params.ColourScale > 0 {
					v := normalise(float64(value), params.ColourScale, t.NoData)
					if v == t.NoData {
						continue
					}
					value = float32(v)
				}

				if i == 0 {
					minVal = value
					maxVal = value
				} else {
					if value < minVal {
						minVal = value
					}

					if value > maxVal {
						maxVal = value
					}
				}
			}

			if minVal == maxVal {
				maxVal += 0.1
			}

			offset = minVal
			clip = maxVal
		}

		trange := clip - offset

		for i, value := range t.Data {
			if value == noData {
				out.Data[i] = 0xFF
			} else {
				if params.ColourScale > 0 {
					v := normalise(float64(value), params.ColourScale, t.NoData)
					if v == t.NoData {
						out.Data[i] = 0xFF
						continue
					}
					value = float32(v)
				}

				c := uint8(math.Floor(float64(((value - clip) / trange * (float32(steps) - 1)))))

				if c < 0 {
					out.Data[i] = 0xFF
					continue
				} else if c > 255 {
					out.Data[i] = 0xFF
					continue
				}

				out.Data[i] = c
			}
		}
		return out, nil

	default:
		return &ByteRaster{}, fmt.Errorf("Raster type not implemented")
	}
}

func ScaleNew(rs []Raster, params ScaleParams) ([]*ByteRaster, error) {
	out := make([]*ByteRaster, len(rs))

	for i, r := range rs {
		br, err := scaleNew(r, params)
		if err != nil {
			return out, err
		}
		out[i] = br
	}

	return out, nil
}
