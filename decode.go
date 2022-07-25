package jpeg

import (
    "fmt"
    "os"
    "bufio"
    "math"
)

// must be called after all scans have been processed for a single frame
func (jpg *Desc) dequantize( f *frame ) error {

    for _, cmp := range f.components {          // for each component in frame

        if cmp.QS > 3 { return fmt.Errorf("dequantize: table out of range\n") }
        qz := jpg.qdefs[cmp.QS]

        for _, duRow := range cmp.iDCTdata {    // for each DU row
            for k := 0; k < len(duRow); k++ {   // for each data unit
                du := &duRow[k]                 // pointer to du (du is updated)
                var uZZdu dataUnit              // temporary storage
                i := 0
                for r := 0; r < 8; r ++ {       // dequantize DCT coefficients
                    for c := 0; c < 8; c ++ {
                        j := zigZagRowCol[r][c]
                        uZZdu[i] = du[j] * int16(qz.values[j])
                        i ++
                    }
                }
                for i := 0; i < 64; i++ {       // unZigZag Coefficients
                    du[i] = uZZdu[i]
                }
            }
        }
    }
    return nil
}

const(
    is0 = 2.828427124746190097603377448419
    is1 = 3.923141121612921796504728944537
    is2 = 3.695518130045147024512732757587
    is3 = 3.325878449210180948315153510472
    is4 = 2.828427124746190097603377448419
    is5 = 2.222280932078408898971323255794
    is6 = 1.530733729460359086913839936122
    is7 = 0.780361288064513071393139473908

    ia1 = 1.414213562373095048801688724209
    a2 = 0.541196100146196984399723205367
    ia3 = 1.414213562373095048801688724209
    a4 = 1.306562964876376527856643173427
    a5 = 0.382683432365089771728459984030
)

func inverseDCT8( du *dataUnit, start []uint8, stride uint ) {

    var oneD [64]float64
    var u int

    inverseTransform8Col := func( ) {
        v15 := float64(du[u]) * is0
	    v26 := float64(du[u+8]) * is1
	    v21 := float64(du[u+16]) * is2
	    v28 := float64(du[u+24]) * is3
	    v16 := float64(du[u+32]) * is4
	    v25 := float64(du[u+40]) * is5
	    v22 := float64(du[u+48]) * is6
	    v27 := float64(du[u+56]) * is7

        v19 := (v25 - v28) * 0.5
	    v20 := (v26 - v27) * 0.5
	    v23 := (v26 + v27) * 0.5
	    v24 := (v25 + v28) * 0.5

	    v7  := (v23 + v24) * 0.5
	    v11 := (v21 + v22) * 0.5
	    v13 := (v23 - v24) * 0.5
	    v17 := (v21 - v22) * 0.5

	    v8 := (v15 + v16) * 0.5
	    v9 := (v15 - v16) * 0.5

	    term := (v19 - v20) * a5
    //  Using term in expressions and after simplifications leads to
    //  v12 := (v19 * a4 - term) / (a2 * a5 - a2 * a4 - a4 * a5)
    //  v14 := (term - v20 * a2) / (a2 * a5 - a2 * a4 - a4 * a5)
    //  Since it turns out that 1/(a2 * a5 - a2 * a4 - a4 * a5) is -1,
    //  the result is simply:
        v12 := term - v19 * a4
        v14 := v20 * a2 - term

	    v6 := v14 - v7
	    v5 := v13 * ia3 - v6
	    v4 := -v5 - v12
	    v10 := v17 * ia1 - v11

	    v0 := (v8 + v11) * 0.5
	    v1 := (v9 + v10) * 0.5
	    v2 := (v9 - v10) * 0.5
	    v3 := (v8 - v11) * 0.5

	    oneD[u] = (v0 + v7) * 0.5
	    oneD[u+8] = (v1 + v6) * 0.5
	    oneD[u+16] = (v2 + v5) * 0.5
	    oneD[u+24] = (v3 + v4) * 0.5
	    oneD[u+32] = (v3 - v4) * 0.5
	    oneD[u+40] = (v2 - v5) * 0.5
	    oneD[u+48] = (v1 - v6) * 0.5
	    oneD[u+56] = (v0 - v7) * 0.5
    }

    for u = 0; u < 8; u++ {
        inverseTransform8Col( )
    }

    var v int
    inverseTransform8Row := func( ) {
        cv := v << 3
        v15 := oneD[cv] * is0
        v26 := oneD[cv+1] * is1
        v21 := oneD[cv+2] * is2
        v28 := oneD[cv+3] * is3
        v16 := oneD[cv+4] * is4
        v25 := oneD[cv+5] * is5
        v22 := oneD[cv+6] * is6
        v27 := oneD[cv+7] * is7

        v19 := (v25 - v28) * 0.5
        v20 := (v26 - v27) * 0.5
        v23 := (v26 + v27) * 0.5
        v24 := (v25 + v28) * 0.5

        v7  := (v23 + v24) * 0.5
        v11 := (v21 + v22) * 0.5
        v13 := (v23 - v24) * 0.5
        v17 := (v21 - v22) * 0.5

        v8 := (v15 + v16) * 0.5
        v9 := (v15 - v16) * 0.5

        term := (v19 - v20) * a5
        //  Using term in expressions and after simplifications leads to
        //  v12 := (v19 * a4 - term) / (a2 * a5 - a2 * a4 - a4 * a5)
        //  v14 := (term - v20 * a2) / (a2 * a5 - a2 * a4 - a4 * a5)
        //  Since it turns out that 1/(a2 * a5 - a2 * a4 - a4 * a5) is -1,
        //  the result is simply:
        v12 := term - v19 * a4
        v14 := v20 * a2 - term

        v6 := v14 - v7
        v5 := v13 * ia3 - v6
        v4 := -v5 - v12
        v10 := v17 * ia1 - v11

        v0 := (v8 + v11) * 0.5
        v1 := (v9 + v10) * 0.5
        v2 := (v9 - v10) * 0.5
        v3 := (v8 - v11) * 0.5

        val := int(math.Round((v0 + v7) * 0.5)) + 128
        if val < 0 { val = 0 } else if val > 255 { val = 255 }
        start[0] = uint8(val)

        val = int(math.Round((v1 + v6) * 0.5)) + 128
        if val < 0 { val = 0 } else if val > 255 { val = 255 }
        start[1] = uint8(val)

        val = int(math.Round((v2 + v5) * 0.5)) + 128
        if val < 0 { val = 0 } else if val > 255 { val = 255 }
        start[2] = uint8(val)

        val = int(math.Round((v3 + v4) * 0.5)) + 128
        if val < 0 { val = 0 } else if val > 255 { val = 255 }
        start[3] = uint8(val)

        val = int(math.Round((v3 - v4) * 0.5)) + 128
        if val < 0 { val = 0 } else if val > 255 { val = 255 }
        start[4] = uint8(val)

        val = int(math.Round((v2 - v5) * 0.5)) + 128
        if val < 0 { val = 0 } else if val > 255 { val = 255 }
        start[5] = uint8(val)

        val = int(math.Round((v1 - v6) * 0.5)) + 128
        if val < 0 { val = 0 } else if val > 255 { val = 255 }
        start[6] = uint8(val)

        val = int(math.Round((v0 - v7) * 0.5)) + 128
        if val < 0 { val = 0 } else if val > 255 { val = 255 }
        start[7] = uint8(val)
    }

    for v = 0; v < 8; v++ {
        inverseTransform8Row( )
        if uint(len(start)) > stride { start = start[stride:] }
    }
}

/*
func inverseDCT8( du *dataUnit, start []uint8, stride uint ) {
    for x := 0; x < 8; x++ {
        for y := 0; y < 8; y++ {

            var res float64 = 0.0

            for u := 0; u < 8; u++ {
                var alphaU float64
                if u == 0 {
                    alphaU = 1.0/math.Sqrt2
                } else {
                    alphaU = 1.0
                }
                // unroll innermost loop
                uvIndex := u << 3  // u * 3
                xuFloat := float64((2*x + 1)*u)
                yvFloat := float64(2*y + 1)     // starting at v = 1
                res += (alphaU / math.Sqrt2) * float64(du[uvIndex]) *   // v = 0
                        math.Cos( (math.Pi * xuFloat) / 16.0 )
                res += alphaU * float64(du[uvIndex+1]) *                // v = 1
                        math.Cos( (math.Pi * xuFloat) / 16.0 ) *
                        math.Cos( (math.Pi * yvFloat) / 16.0 )
                res += alphaU * float64(du[uvIndex+2]) *                // v = 2
                        math.Cos( (math.Pi * xuFloat) / 16.0 ) *
                        math.Cos( (math.Pi * yvFloat) / 8.0 )
                res += alphaU * float64(du[uvIndex+3]) *                // v = 3
                        math.Cos( (math.Pi * xuFloat) / 16.0 ) *
                        math.Cos( (math.Pi * yvFloat * 3.0) / 16.0 )
                res += alphaU * float64(du[uvIndex+4]) *                // v = 4
                        math.Cos( (math.Pi * xuFloat) / 16.0 ) *
                        math.Cos( (math.Pi * yvFloat) / 4.0 )
                res += alphaU * float64(du[uvIndex+5]) *                // v = 5
                        math.Cos( (math.Pi * xuFloat) / 16.0 ) *
                        math.Cos( (math.Pi * yvFloat * 5.0) / 16.0 )
                res += alphaU * float64(du[uvIndex+6]) *                // v = 6
                        math.Cos( (math.Pi * xuFloat) / 16.0 ) *
                        math.Cos( (math.Pi * yvFloat * 6.0) / 16.0 )
                res += alphaU * float64(du[uvIndex+7]) *                // v = 7
                        math.Cos( (math.Pi * xuFloat) / 16.0 ) *
                        math.Cos( (math.Pi * yvFloat * 7.0) / 16.0 )
            }

            res /= 4.0
            val := int(math.Round(res)) + 128
            if val < 0 { val = 0 } else if val > 255 { val = 255 }
            start[y] = uint8(val)
        }
//        if x < 7 { start = start[stride:] }
        if uint(len(start)) >= stride { start = start[stride:] }
//        fmt.Printf( "End array %d\n", len(start) )
    }
}
*/

func (jpg *Desc) GetImageOrientation( ) (*Orientation, error) {
    if jpg.orientation == nil {
        return nil, fmt.Errorf( "GetImageOrientation: no orientation information\n" )
    }
    return jpg.orientation, nil
}

func make8BitComponentArrays( cmps []component ) [](*[]uint8) {

    cArrays := make( [](*[]uint8), len( cmps ) ) // one flat []byte per component

    for cdi, cmp := range cmps {    // for each component
        rows := cmp.iDCTdata        // 1 slice of same length rows of dataUnits
        cArray := make ( []uint8, uint(len(rows)) * cmp.nUnitsRow * 64 )
        cArrays[cdi] = &cArray

//fmt.Printf( "Cmp %d, nRows %d nUnitsRow %d sample array size %d\n",
//            cdi, len(rows), cmp.nUnitsRow, len(cArray))
        stride := cmp.nUnitsRow << 3                // 8 samples per dataUint
        for r, row := range rows {
            start := (uint(r) * cmp.nUnitsRow) << 6 // row origin in samples
//fmt.Printf( "Row %d starting @ %d\n", r, start)
            for c := 0; c < len(row); c ++ {
                index := start + (uint(c) << 3)    // du origin in row samples
//fmt.Printf("Accessing DU %d in row %d start index %d end @ %d stride %d\n",
//            c, r, index, len(cArray), stride)
                inverseDCT8( &row[c], cArray[index:], stride )
            }
        }
    }
    return cArrays
}

func (jpg *Desc) MakeFrameRawPicture( frame int ) ([](*[]uint8), error) {
    if frame >= len(jpg.frames) || frame < 0 {
        return nil, fmt.Errorf( "MakeFrameRawPicture: frame %d is absent\n", frame )
    }
    frm := &jpg.frames[frame]
    if len( frm.scans ) < 1 {
        return nil, fmt.Errorf( "SaveRawPicture: no scan available for picture\n" )
    }
    if err := jpg.dequantize( frm ); err != nil {
        return nil, err
    }

    cmps := frm.components
    var samples [](*[]uint8)
    switch frm.resolution.samplePrecision {
    case 8:
        samples = make8BitComponentArrays( cmps )
    default:
        return nil, fmt.Errorf( "MakeFrameRawPicture: extended precision is not supported\n" )
    }
    return samples, nil
}

const writeBufferSize = 1048576
func (jpg *Desc) writeBW( f *os.File, frm *frame, samples [](*[]uint8),
                          o *Orientation ) (nc, nr uint, n int, err error) {

    bw := bufio.NewWriterSize( f, writeBufferSize )
    cbw := newCumulativeWriter( bw )

    cols := uint(frm.resolution.nSamplesLine)
    rows := uint(frm.resolution.nLines)

    Y := samples[0]
    yStride := frm.components[0].nUnitsRow << 3

    writePixel := func( r, c uint ) {
        if c < cols && r < rows {
            ys  := (*Y)[r*yStride+c]
            cbw.Write( []byte{ ys, ys, ys } )
        }
    }

    nSamples  := uint(len(*Y))
    sampleRows := nSamples / yStride

    var writeOrientedBW func()

    if o == nil || (o.Row0 == Top && o.Col0 == Left ) { // default orientation
        nr = rows
        nc = cols
        writeOrientedBW = func() {
            for i := uint(0); i < nSamples; i++ {
                writePixel( i / yStride, i % yStride )
            }
        }
    } else if o.Row0 == Top && o.Col0 == Right {
        nr = rows
        nc = cols
        cStart := yStride - 1
        writeOrientedBW = func () {
            for i := uint(0);i < nSamples; i++ {
                writePixel( i / yStride, cStart - (i % yStride) )
            }
        }
    } else if o.Row0 == Right && o.Col0 == Top {        // rotation +90
        nr = cols
        nc = rows
        rStart := sampleRows - 1
        writeOrientedBW = func () {
            for i := uint(0);i < nSamples; i++ {
                writePixel( rStart - (i % sampleRows), i / sampleRows )
            }
        }
    } else if o.Row0 == Right && o.Col0 == Bottom {
        nr = cols
        nc = rows
        rStart := sampleRows - 1
        cStart := yStride - 1
        writeOrientedBW = func () {
            for i := uint(0);i < nSamples; i++ {
                writePixel( rStart - i % sampleRows, cStart - (i / sampleRows) )
            }
        }
    } else if o.Row0 == Bottom && o.Col0 == Left {
        nr = rows
        nc = cols
        rStart := sampleRows - 1
        writeOrientedBW = func () {
            for i := uint(0);i < nSamples; i++ {
                writePixel( rStart - (i / yStride), i % yStride )
            }
        }
    } else if o.Row0 == Bottom && o.Col0 == Right {
        nr = rows
        nc = cols
        rStart := sampleRows - 1
        cStart := yStride - 1
        writeOrientedBW = func () {
            for i := uint(0);i < nSamples; i++ {
                writePixel( rStart - (i / yStride), cStart - (i % yStride) )
            }
        }
    } else if o.Row0 == Left && o.Col0 == Top {
        nr = cols
        nc = rows
        writeOrientedBW = func() {
            for i := uint(0); i < nSamples; i++ {
                writePixel( i % sampleRows, i / sampleRows )
            }
        }
    } else if o.Row0 == Left && o.Col0 == Bottom {      // rotation -90
        nr = cols
        nc = rows
        cStart := yStride - 1
        writeOrientedBW = func() {
            for i := uint(0); i < nSamples; i++ {
                writePixel( i % sampleRows, cStart - (i / sampleRows) )
            }
        }
    }

    writeOrientedBW( )
    n, err = cbw.result()
    err = bw.Flush()
    return
}

func (jpg *Desc) writeYCbCr( f *os.File, frm *frame, samples [](*[]uint8),
                             o *Orientation ) (nc, nr uint, n int, err error) {
    if len(samples) != 3 {  // contract: writeYCbCr requires 3 components
        panic("writeYCbCr: incorrect number of components\n")
    }

    bw := bufio.NewWriterSize( f, writeBufferSize )
    cbw := newCumulativeWriter( bw )

    cols  := uint(frm.resolution.nSamplesLine)
    rows  := uint(frm.resolution.nLines)

    Y := samples[0]
    Cb := samples[1]
    Cr := samples[2]

    cmps := frm.components
    yHSF := uint(cmps[0].HSF)
    yVSF := uint(cmps[0].VSF)
    yStride := cmps[0].nUnitsRow << 3

    CbHSF := uint(cmps[1].HSF)
    CbVSF := uint(cmps[1].VSF)
    CbStride := cmps[1].nUnitsRow << 3

    CrHSF := uint(cmps[2].HSF)
    CrVSF := uint(cmps[2].VSF)
    CrStride := cmps[2].nUnitsRow << 3
//fmt.Printf("yHSF %d, CbHSF %d, CrHSF %d, yVSF %d, CbVSF %d, CrVSF %d, CbStride %d, CrStride %d\n",
//            yHSF, CbHSF, CrHSF, yVSF, CbVSF, CrVSF, CbStride, CrStride )

    // Assuming yHSF and yVSF are >= Cb/Cr H/V SF:
    // Destination is an array of packed RGB values, indexed by i [0..len[Y]]
    // Sources are Y, Cb and Cr arrays indexed such that given source row r and
    // col c, sample Ys is directly y[j] whereas samples Cbs and Crs are given
    // by C{b/r}s = Cb[((*rC{b/r}VSF)/yVSF)*CbStride + (c*C{b/r}HSF)/yHSF])
    // Depending on actual orientation (Row0 and Col0) the source row r and col
    // c are calculated from the destination index i

    writePixel := func( r, c uint ) {
        if c < cols && r < rows {
            Ys  := float32((*Y)[r*yStride+c])
            Cbs := float32((*Cb)[((r*CbVSF)/yVSF)*CbStride + (c*CbHSF)/yHSF])
            Crs := float32((*Cr)[((r*CrVSF)/yVSF)*CrStride + (c*CrHSF)/yHSF])

            rs := int( 0.5 + Ys + 1.402*(Crs-128.0) )
            if rs < 0 { rs = 0 } else if rs > 255 { rs = 255 }
            gs := int( 0.5 + Ys - 0.34414*(Cbs-128.0) - 0.71414*(Crs-128.0) )
            if gs < 0 { gs = 0 } else if gs > 255 { gs = 255 }
            bs := int( 0.5 + Ys + 1.772*(Cbs-128.0) )
            if bs < 0 { bs = 0 } else if bs > 255 { bs = 255 }

            cbw.Write( []byte{ byte(rs), byte(gs), byte(bs) } )
        }
    }

    var writeOrientedRGB func()
    nSamples  := uint(len(*Y))
    sampleRows := nSamples / yStride

    if o == nil || (o.Row0 == Top && o.Col0 == Left ) { // default orientation
        nr = rows
        nc = cols
        writeOrientedRGB = func() {
            for i := uint(0); i < nSamples; i++ {
                writePixel( i / yStride, i % yStride )
            }
        }
    } else if o.Row0 == Top && o.Col0 == Right {
        nr = rows
        nc = cols
        cStart := yStride - 1
        writeOrientedRGB = func () {
            for i := uint(0);i < nSamples; i++ {
                writePixel( i / yStride, cStart - (i % yStride) )
            }
        }
    } else if o.Row0 == Right && o.Col0 == Top {        // rotation +90
        nr = cols
        nc = rows
        rStart := sampleRows - 1
        writeOrientedRGB = func () {
            for i := uint(0);i < nSamples; i++ {
                writePixel( rStart - (i % sampleRows), i / sampleRows )
            }
        }
    } else if o.Row0 == Right && o.Col0 == Bottom {
        nr = cols
        nc = rows
        rStart := sampleRows - 1
        cStart := yStride - 1
        writeOrientedRGB = func () {
            for i := uint(0);i < nSamples; i++ {
                writePixel( rStart - i % sampleRows, cStart - (i / sampleRows) )
            }
        }
    } else if o.Row0 == Bottom && o.Col0 == Left {
        nr = rows
        nc = cols
        rStart := sampleRows - 1
        writeOrientedRGB = func () {
            for i := uint(0);i < nSamples; i++ {
                writePixel( rStart - (i / yStride), i % yStride )
            }
        }
    } else if o.Row0 == Bottom && o.Col0 == Right {
        nr = rows
        nc = cols
        rStart := sampleRows - 1
        cStart := yStride - 1
        writeOrientedRGB = func () {
            for i := uint(0);i < nSamples; i++ {
                writePixel( rStart - (i / yStride), cStart - (i % yStride) )
            }
        }
    } else if o.Row0 == Left && o.Col0 == Top {
        nr = cols
        nc = rows
        writeOrientedRGB = func() {
            for i := uint(0); i < nSamples; i++ {
                writePixel( i % sampleRows, i / sampleRows )
            }
        }
    } else if o.Row0 == Left && o.Col0 == Bottom {      // rotation -90
        nr = cols
        nc = rows
        cStart := yStride - 1
        writeOrientedRGB = func() {
            for i := uint(0); i < nSamples; i++ {
                writePixel( i % sampleRows, cStart - (i / sampleRows) )
            }
        }
    }
//    start := time.Now()
    writeOrientedRGB()
//    stop := time.Now()
//    fmt.Printf( "writeYCbCr: elapsed time %d\n", stop.Sub(start) )
    n, err = cbw.result()
    err = bw.Flush()
    return
}

func (jpg *Desc) SaveRawPicture( path string, bw bool,
                                 ort *Orientation ) ( nCols, nRows uint,
                                                      n int, err error) {
    if ! jpg.IsComplete() || len(jpg.frames) == 0 {
        return 0, 0, 0, fmt.Errorf( "SaveRawPicture: no frame to save\n" )
    }
    if len(jpg.frames) > 1 {
        return 0, 0, 0, fmt.Errorf( "SaveRawPicture: multiple frames are not supported\n" )
    }
    frm := &jpg.frames[0]
    if len( frm.scans ) < 1 {
        return 0, 0, 0, fmt.Errorf( "SaveRawPicture: no scan available for picture\n" )
    }

    if err = jpg.dequantize( frm ); err != nil {
        return 0, 0, 0, err
    }

    cmps := frm.components
    var samples [](*[]uint8)
    switch frm.resolution.samplePrecision {
    case 8:
        samples = make8BitComponentArrays( cmps )
    default:
        return 0, 0, 0, fmt.Errorf( "SaveRawPicture: extended precision is not supported\n" )
    }
    var f *os.File
    f, err = os.OpenFile( path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.ModePerm)
    if err != nil {
        return 0, 0, 0, err
    }
    defer func ( ) { if e := f.Close(); err == nil { err = e } }()
    switch len( cmps ) {
    case 3:
        if ! bw {
            nCols, nRows, n, err = jpg.writeYCbCr( f, frm, samples, ort )
            break
        }
        fallthrough
    case 1: nCols, nRows, n, err = jpg.writeBW( f, frm, samples, ort )
    default:
        err = fmt.Errorf("SaveRawPicture: not YCbCr or Gray scale picture\n")
    }
    return
}

