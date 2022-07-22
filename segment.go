package jpeg

import (
    "fmt"
    "bytes"
    "io"
    "encoding/binary"
)

/*
    A picture is split into small squares of 8 * 8 pixels. In the usual case of
    color pictures, the image is made of pixels with 3 components, Luminance Y
    and 2 Chrominance (Cb, Cr) values. Each 8 * 8 pixel block is then split in
    data units, where each data unit is made of a single component samples,
    64 samples in a square

    An image may be received as a single scan that includes pixels of those 3
    components (interleaved data units) or as multiple scans, each including
    only pixels from one component (non-interleaved data units). If the image
    is grayscale, there is only 1 component Y and the scan is non-interleaved.

    MCU is the Minimum Coded Unit in scan Entropy Coded Segments (ECS). Each
    MCU describes the number and location of encoded data units.

    If the scan is non-interleaved, one MCU is just one data unit (64 samples),
    otherwise it is a series of Y, Cb, Cr data units. MCU gives the number of
    included data units and for each data unit the relative location of the
    data unit in the complete image.

    Because scans are made of complete data units, and even if the number of
    lines or the number of samples per line in the frame is not a multiple of
    8, a scan must include samples for all data units than start within the
    image. The location of each data unit is given by its row and column. In
    case of interleaved data units, component resolutions may differ and for
    example the luminance component may require more data units than other
    chrominance components. In that case each MCU includes more Y data units
    than Cb or Cr data units, and the MCU "position" in the image is defined
    by its anchor.

    For example, in the typical case of 4, 2, 0 chroma subsampling, the first
    4 data units are LUMA (the first in the current luma anchor location, the
    second in the current luma anchor location + 1 on the same row, the third
    in the current luma anchor location on the row below and the fourth in the
    current luma anchor location + 1 on the row below), the fifth is chroma
    Cb (in the current Cb anchor location) and the sixth is the chroma Cr (in
    the current Cb anchor location). Once a MCU is completed, the luma anchor
    location in incremented by two in column, whereas both Cb and Cr anchor
    locations are incremented by one in column, until the end of their row.

    In case of interleaved scans, for each component the end of row is the
    number of component data units per line, that is:
        ceiling( samples per line * comp HSF / ( max HSF * 8 ) )
    Were comp HSF stands for the component horizontal sampling factor and
    max HSF for the maximum horizontal samplig factor of all components.

    However, sometimes the value of samples/line given in the SOF header is not
    aligned with the restart marker intervals, if restart markers are used. In
    case of disagreement, the number of data units in a row is aligned on the
    restart interval in order to make enough room for all data units in a scan
    segment (between 2 restart intervals).

    In that case the end of row is the number of MCUs between 2 restart markers
    (restart interval) multiplied by the number of data units in the MCU, on a
    component per component basis:  nMcuRST * comp HSF

    scanComp drives decompression by providing the huffman tables for each
    data unit in the MCU, the number of units per row and for each data unit
    the location of each decoded sample in the image.

    However, DC and AC samples are preprocessed and in particular AC samples
    are runlength compressed before entropy compression: a single preprocessed
    AC sample can represent many 0 samples - EOB indicates that all following
    samples are 0 until the end of block (64), ZRL indicates that the sixteen
    following samples are 0 and any non-zero sample can be preceded by up to
    15 zero samples. In case of progressive scans, a single AC sample may even
    jump over many data units.
*/

type scanCompRef struct {      // scan component reference
    cmId, dcId, acId uint8
}

func getCoefNames( start, end uint8 ) (coefs string) {

    if start == 0 {
        if end == 0 {
            coefs = "DC only"
        } else {
            coefs = fmt.Sprintf( "DC and AC[%d..%d]", start+1, end )
        }
    } else {
        coefs = fmt.Sprintf( "AC[%d..%d] only", start, end )
    }
    return
}

func getPointTransform( h, l uint8 ) (pt uint8) {
    if h == 0 {
        pt = 1 << l;
    } else {
        pt = 1 << l;
    }
    return
}

var componentNames = [...]string{ "Y", "Cb", "Cr" }
func (jpg *Desc) setScan( s *scan, sComp *[]scanCompRef ) error {

    frm := jpg.getCurrentFrame()
    if frm == nil { panic( "No frame for scan\v" ) }

    nComp := len( *sComp )
    fmt.Printf( "Scan: %d component(s)\n", nComp )
    fmt.Printf( "  Spectral Selection start: %d, end: %d coefficients: %s\n",
                s.startSS, s.endSS, getCoefNames( s.startSS, s.endSS ) )
    fmt.Printf( "  Sucessive Approximation Ah: %d, Al: %d point transform *%d\n",
                s.sABPh, s.sABPl, getPointTransform( s.sABPh, s.sABPl ) )

    s.sComps = make( []scanComp, nComp )
    for i, sc := range( *sComp ) {
        var cmp *component = nil;
        for j, _ := range( frm.components ) {   // in fixed order Y, Cb, Cr
            if sc.cmId == frm.components[j].Id {
                cmp = &frm.components[j]
                s.sComps[i].cType = uint8(j)
                fmt.Printf( "  Component #%d id %d [%s]\n", i, sc.cmId, componentNames[j] )
            }
        }
        if cmp == nil {
            return fmt.Errorf( "Unknown component id %d for scan\n", sc.cmId );
        }
        s.sComps[i].iDCTdata = &cmp.iDCTdata
        s.sComps[i].cId = cmp.Id

        qsz := uint8(jpg.qdefs[cmp.QS].size)
        if qsz == 0 {
            return fmt.Errorf( "Missing Quantization table %d for scan\n",
                               cmp.QS )
        }
        if qsz != frm.resolution.samplePrecision {
            return fmt.Errorf( "Quantization size %d does not match frame sample size (%d)\n",
                               qsz, frm.resolution.samplePrecision )
        }

        if s.startSS == 0 {
            fmt.Printf( "    Huffman DC Id: %d\n", sc.dcId )
            s.sComps[i].hDC = jpg.hdefs[2*sc.dcId].root   // AC follows DC
            if s.sComps[i].hDC == nil {
                return fmt.Errorf( "Missing Huffman table %d for DC scan (component %d)\n",
                                   sc.dcId, i )
            }
        }
        s.sComps[i].dcId = sc.dcId

        if s.endSS > 0 {
            fmt.Printf( "    Huffman AC Id: %d\n", sc.acId )
            s.sComps[i].hAC = jpg.hdefs[2*sc.acId+1].root // (2 tables per dest)
            if s.sComps[i].hAC == nil {
                return fmt.Errorf( "Missing Huffman table %d for AC scan (component %d)\n",
                                   sc.acId, i )
            }
        }
        s.sComps[i].acId = sc.acId

        // In case of multi-component scans, copy useful data from the component
        // definition into the scanComp data structure
        if nComp > 1 {
            s.sComps[i].HSF, s.sComps[i].VSF, s.sComps[i].nUnitsRow =
                    cmp.HSF,         cmp.VSF,         cmp.nUnitsRow
        } else {
            s.sComps[i].HSF, s.sComps[i].VSF = 1, 1
            // calculate the number of data Units per line
            roundingFactor := (uint16(frm.resolution.mhSF) * 8) / uint16(cmp.HSF)
            s.sComps[i].nUnitsRow = uint((frm.resolution.nSamplesLine +
                                                        roundingFactor - 1) /
                                                                roundingFactor)
        }
        fmt.Printf( "    HSF %d, VSF %d, nUnitsRow %d\n",
                    s.sComps[i].HSF, s.sComps[i].VSF, s.sComps[i].nUnitsRow )
        // All other fields are intialized to 0
    }
    return nil
}

func subsamplingFormat( sc *scan ) string {
    // Chroma subsampling formula (4:a:b), where:
    // 4 is fixed number of Y samples per line and 2 is fixed number of lines
    // a is the number of CbCr samples in the 1st line
    // b is the number of changes between 1st and 2nd line
    // The assumption for Chroma subsampling is that numbers for Chroma are at
    // best the same as for Luma, but usually lower.
    // In JPEG, Y, Cb, Cr are given as Horizontal & Vertical sampling factors
    // Different values for Cb and Cr are possible, and in theory they could be
    // higher than those for Luma. This would not be expressible with the
    // standard subsampling formula: Cb and Cr must have the same sampling
    // factors to fit in the formula. The sampling factors that are compatible
    // with the standard subsampling formula are:
    // Y(1:1), Cb(1:1), Cr(1:1) => 4:4:4 No subsampling
    // Y(1:2), Cb(1:1), Cr(1:1) => 4:4:0 Chroma 1/2 vertically
    // Y(2:1), Cb(1:1), Cr(1:1) => 4:2:2 Chroma 1/2 horizontally
    // Y(2:2), Cb(1:1), Cr(1:1) => 4:2:0 Chroma 1/2 vertically & horizontally
    // Y(4:1), Cb(1:1), Cr(1:1) => 4:1:1 Chroma 1/4 horizontally
    // Y(4:2), Cb(1:1), Cr(1:1) => 4:1:0 Chroma 1/2 vertically 1/4 horizontally
    // Where Component(n:m) indicates the component H:V sampling factor
    //
    // The number of Y samples is fixed to 4, so if nLuma is not 4 a coefficient
    // must be applied for the number of chroma samples (4/LumaS). The number of
    // chroma changes between the first and second line is indicated by the
    // chroma vertical sampling factor -1 (if the sampling factor is 1, there
    // is no change between the 2 lines). However, if the Y vertical sampling
    // factor is not 2, the coefficient 2/nLumaLines must be applied:
    // Therefore: a = (chromaS * 4) / lumaS
    //            b = a * (((chromaL * 2) / lumaL) - 1)
    // Those formulae could work for any nluma and nlumaLines above 4 and 3, but
    // the calculation would have to be done in float, before being turned back
    // to integers.
    if len( sc.sComps ) < 2 {
        return ""   // no chroma
    }
    lumaS := sc.sComps[0].HSF
    lumaL := sc.sComps[0].VSF
    chromaS := sc.sComps[1].HSF
    chromaL := sc.sComps[1].VSF

    if len( sc.sComps ) == 3 &&
        chromaS !=  sc.sComps[2].HSF &&
        chromaL !=  sc.sComps[2].VSF {
        return ""   // not representable
    }
    a := (chromaS * 4) / lumaS
    b := a * ( ( ( chromaL * 2 ) / lumaL ) - 1 )
    return fmt.Sprintf( "4:%d:%d", a, b )
}

func makeCompString( comp string, h, v uint8 ) string {
    var cs []byte = make( []byte, 64 )  // max 4 samples comp => 16 * 4
    j := int(0)
    for row := uint8(0); row < v; row ++ {
        for col := uint8(0); col < h; col++ {
            n := copy( cs[j:], comp )
            j += n
            cs[j] = byte(row + '0')
            cs[j+1] = byte(col + '0')
            j += 2
        }
    }
    return string(cs[:j])
}

func mcuFormat( sc *scan ) string {

    nCmp := len( sc.sComps )
    if nCmp != 3 && nCmp != 1 { panic("Unsupported MCU format\n") }

    luma := makeCompString( "Y", sc.sComps[0].HSF, sc.sComps[0].VSF )
    var mcuf string
    if nCmp == 3 {
        chromaB := makeCompString( "Cb",
                                sc.sComps[1].HSF, sc.sComps[1].VSF )
        chromaR := makeCompString( "Cr",
                                sc.sComps[2].HSF, sc.sComps[2].VSF )
        mcuf = fmt.Sprintf( "%s%s%s", luma, chromaB, chromaR )
    } else {
        mcuf = luma
    }
    return mcuf
}

const (
    markerLengthSize = 4
    fixedFrameHeaderSize = 8    // all sizes excluding marker, if any
    frameComponentSpecSize = 3
    fixedScanHeaderSize = 6
    scanComponentSpecSize = 2
    restartIntervalSize = 4
    defineNumberOfLinesSize = 4
    fixedCommentHeaderSize = 2
)

// -------------- Frames

func (f *frame)entropyCoding( ) EntropyCoding {
    return EntropyCoding(f.encoding / 8)
}

func (f *frame)encodingMode( ) EncodingMode {
    return EncodingMode(f.encoding % 4)
}

func (f *frame)samplePrecision( ) uint {
    return uint(f.resolution.samplePrecision)
}

func (f *frame)nSamplesLine( ) uint {
    return uint(f.resolution.nSamplesLine)
}

func (f *frame)nLines( ) uint {
    return uint(f.resolution.nLines)
}

func (f *frame)actualLines( ) (nLines uint16) {
    if f.resolution.scanLines != 0 {
        nLines = f.resolution.scanLines
    } else if f.resolution.dnlLines != 0 {
        nLines = f.resolution.dnlLines
    } else {
        nLines = f.resolution.nLines
    }
    return
}

func (f *frame)serialize( w io.Writer ) (int, error) {

    lf := uint16((len(f.components) * frameComponentSpecSize) + fixedFrameHeaderSize)
    seg := make( []byte, lf + 2 )
    binary.BigEndian.PutUint16( seg[0:], uint16(f.encoding)+_SOF0 )
    binary.BigEndian.PutUint16( seg[2:], lf )
    seg[4] = byte(f.resolution.samplePrecision)

    binary.BigEndian.PutUint16( seg[5:], f.actualLines() )
    binary.BigEndian.PutUint16( seg[7:], f.resolution.nSamplesLine )
    seg[9] = byte(len(f.components))

    i := 10
    for _, c := range f.components {
        seg[i] = byte(c.Id)
        seg[i+1] = byte( (c.HSF << 4) + c.VSF )
        seg[i+2] = byte(c.QS)
        i += 3
    }
    return w.Write( seg )
}

func (f *frame)format( w io.Writer ) (n int, err error) {
    cw := newCumulativeWriter( w )
    cw.format( "  Frame Encoding: %s\n", encodingString(f.encoding) )
    cw.format( "    Entropy Coding: %s\n", entropyCodingString(f.entropyCoding()) )
    cw.format( "    Encoding Mode: %s\n", encodingModeString(f.encodingMode()) )
    nSamples := f.resolution.nSamplesLine
    cw.format( "    Lines: %d, Samples/Line: %d," +
               " sample precision: %d-bit, components: %d\n",
               f.actualLines(), nSamples,
               f.resolution.samplePrecision, len( f.components ) )
//    if ( nSamples % 8) != 0 {
//        cw.format( "    Warning: Samples/Line (%d) is not a multiple of 8\n",
//                   nSamples )
//    }
    nMCUsLine := uint16(f.image.nMcuRST)
    if nMCUsLine != 00 && (nSamples % nMCUsLine) != 0 {
        cw.format( "    Warning: Samples/Line (%d) is not a " +
                   "multiple of the restart interval (%d)\n", nSamples, nMCUsLine )
    }

    for i, c := range f.components {
        cw.format( "      Component #%d Id %d Sampling factors"+
                   " H:V=%d:%d, Quantization selector %d\n",
                   i, c.Id, c.HSF, c.VSF, c.QS )
    }
    n, err = cw.result()
    if err != nil { err = fmt.Errorf( "format: %w", err ) }
    return
}

func (jpg *Desc) startOfFrame( marker uint, sLen uint ) error {

    if jpg.state != _FRAME && jpg.state != _APPLICATION {
        return fmt.Errorf( "startOfFrame: Wrong sequence %s in state %s\n",
                           getJPEGmarkerName(marker), jpg.getJPEGStateName() )
    }
    if sLen < fixedFrameHeaderSize {
        return fmt.Errorf( "startOfFrame: Wrong SOF%d header (len %d)\n", marker & 0x0f, sLen )
    }
    offset := jpg.offset + markerLengthSize
    nComponents := uint(jpg.data[offset+5])
    if sLen < fixedFrameHeaderSize + (nComponents * frameComponentSpecSize) {
        return fmt.Errorf( "startOfFrame: Wrong SOF%d header (len %d for %d components)\n",
                           marker & 0x0f, sLen, nComponents )
    }

    jpg.frames = append( jpg.frames,
                         frame {
                           id: uint(len(jpg.frames)),
                           encoding: Encoding(marker & 0x0f),
                           resolution: sampling{
                                samplePrecision: jpg.data[offset],
                                nLines:       uint16(jpg.data[offset+1]) << 8 +
                                              uint16(jpg.data[offset+2]),
                                nSamplesLine: uint16(jpg.data[offset+3]) << 8 +
                                              uint16(jpg.data[offset+4]) },
                           image: jpg } )
    frm := &jpg.frames[len(jpg.frames)-1]
    offset += 6

    var maxHSF, maxVSF uint8
    for i := uint(0); i < nComponents; i++ {
        cId := jpg.data[offset]
        hSF := jpg.data[offset+1]
        vSF := hSF & 0x0f
        hSF >>= 4
        QS := jpg.data[offset+2]

        if hSF > maxHSF { maxHSF = hSF }
        if vSF > maxVSF { maxVSF = vSF }

        frm.components = append( frm.components,
                component{ Id: cId, HSF: hSF, VSF: vSF, QS: QS } )
        offset += frameComponentSpecSize
    }

    frm.resolution.mhSF = maxHSF
    frm.resolution.mvSF = maxVSF

    // In a row the number of data units must be a multiple of the number of
    // MCUs. Each MCU contains mhSF data units of the main component (usually
    // the Y component) and each data unit contains exactly 8 samples. So the
    // actual number of MCUs that must be encoded in a row is given by
    // nMcuRow = ceiling(nSamplesLine / (mhSF * 8))
    maxSamplesMCU := uint16(maxHSF) * 8
    nMcusRow := (frm.resolution.nSamplesLine + maxSamplesMCU - 1) / maxSamplesMCU
    fmt.Printf( "  Frame: %d samples per line, max horizontal SF %d, nMCUs/row %d\n",
                frm.resolution.nSamplesLine, frm.resolution.mhSF, nMcusRow )

    // In a column the number of data units must be a multiple of the number of
    // MCUs. Each MCU contains mvSF data units of the main component (usually
    // the Y component) and each data unit contains exactly 8 samples. So the
    // actual number of MCUs that must be encoded in a column is given by
    // nMcuCol = ceiling(nLines / (mvSF * 8))
    maxSamplesMCU = uint16(maxVSF * 8) // changed maxSamplesMCU meaning
    nMcusCol := (frm.resolution.nLines + maxSamplesMCU - 1) / maxSamplesMCU
    fmt.Printf( "  Frame: %d lines, max vertical SF %d, nMCUs/col %d\n",
                 frm.resolution.nLines, frm.resolution.mvSF, nMcusCol )
    fmt.Printf( "  Frame: %d components\n", nComponents );
    for i := uint(0); i < nComponents; i++ {
        cmp := &frm.components[i]
        fmt.Printf( "    component %d (%s) id %d:\n", i, componentNames[i], cmp.Id )
        nUnitsRow := uint(nMcusRow) * uint(cmp.HSF)
        cmp.nUnitsRow = nUnitsRow
        fmt.Printf( "      horizontal sampling factor %d nUnitsRow: %d (%d samples)\n",
                    cmp.HSF, nUnitsRow, nUnitsRow * 8 )

        nUnitsCol := uint(nMcusCol) * uint(cmp.VSF)
        if nUnitsCol == 0 {
// FIXME: this is legal => preallocate 0 or some minimum number and allow
//        dynamic extension during scan - it will just be slower...
            panic("Unknown number of lines during scan\n")
        }
        fmt.Printf( "      vertical sampling factor %d nUnitsCol: %d (%d lines)\n",
                    cmp.VSF, nUnitsCol, nUnitsCol * 8 )

        cmp.iDCTdata = make( []iDCTRow, nUnitsCol )
        for j := uint(0); j < nUnitsCol; j++ {
            cmp.iDCTdata[j] = make( []dataUnit, nUnitsRow )
        }
    }

    jpg.addSeg( frm )
    jpg.state = _SCAN1  // expecting DHT, DAC, DQT, DRI, COM, or SOS

    return nil
}

// ----------- Scans

func (s *scan)serialize( w io.Writer ) (int, error) {

    ls := uint16((len(s.sComps) * scanComponentSpecSize) + fixedScanHeaderSize)
    seg := make( []byte, ls + 2 )
    binary.BigEndian.PutUint16( seg, _SOS )
    binary.BigEndian.PutUint16( seg[2:], ls )
    seg[4] = byte(len(s.sComps))

    i := 5
    for _, sc := range s.sComps {
        seg[i] = sc.cId
        seg[i+1] = sc.dcId << 4 + sc.acId
        i += 2
    }
    seg[i] = s.startSS
    seg[i+1] = s.endSS
    seg[i+2] = s.sABPh << 4 + s.sABPl

    n, err := w.Write( seg )
    if err != nil {
        return n, err
    }
    // _SOS segment is followed by actual entropy coded segments
    nb, err := w.Write( s.ECSs )
    if err == nil {
        n += nb
    }
    return n, err
}

func (s *scan)formatMCUs( cw *cumulativeWriter, m FormatMode ) {

    nComponents := len(s.sComps)
    cw.format( "    %d Components:\n", nComponents )
    for _, sc := range s.sComps {
        cw.format( "      %s Selector 0x%x, Sampling factors H:%d V:%d\n",
                   componentNames[sc.cType], sc.cId, sc.HSF, sc.VSF )

        cw.format( "         Tables entropy DC:%d AC:%d\n", sc.dcId, sc.acId )

        if m == Extra || m == Both {
            cw.format( "         allocated %d Data Units, %d iDCT rows\n",
                       len(*sc.iDCTdata) * len((*sc.iDCTdata)[0]),
                       len(*sc.iDCTdata) )
        }
    }
    if m == Extra || m == Both {
        cw.format( "    Spectral selection Start:%d, End:%d\n",
                   s.startSS, s.endSS )
        cw.format( "    Successive approximation bit position, high:%d low:%d\n",
                    s.sABPh, s.sABPl )

        if nComponents > 1 {
            subsampling := subsamplingFormat( s )
            mcuFormat := mcuFormat( s )
            comString := "Interleaved"
            cw.format( "    Subsampling %s - %s MCU format %s\n",
                       subsampling, comString, mcuFormat )
        } else {
            cw.format( "    Single component - non-interleaved MCU\n" )
        }
    }
    cw.format( "    Total %d MCUs in scan\n", s.nMcus )
    if s.rstInterval > 0 {
        cw.format( "    Restart interval every %d MCUs (%d restarts in scan)\n",
                   s.rstInterval, s.rstCount )
        cw.format( "    %d Entropy-coded segments (ECS) in scan\n", s.rstCount + 1)
    } else {
        cw.format( "    1 Entropy-coded segment (ECS) in scan\n" )
    }
}

func (s *scan)formatAt( cw *cumulativeWriter, index int, mode FormatMode ) {
    cw.format( "  Scan #%d:\n", index )
    s.formatMCUs( cw, mode )
}

func (s *scan)format( w io.Writer ) (n int, err error) {
    cw := newCumulativeWriter( w )
    cw.format( "  Scan:\n" )
    s.formatMCUs( cw, Standard )
    n, err = cw.result()
    if err != nil { err = fmt.Errorf( "format: %w", err ) }
    return
}

func (jpg *Desc) processScanHeader( sLen uint, sc *scan ) (err error) {

    offset := jpg.offset + markerLengthSize
    nComponents := uint(jpg.data[offset])

    offset += 1
    if sLen != fixedScanHeaderSize + nComponents * scanComponentSpecSize {
        return fmt.Errorf(
            "processScanHeader: Wrong SOS header (len %d for %d components)\n",
            sLen, nComponents )
    }
//    fmt.Printf( "Scan %d Components\n", nComponents )
    sCs := make( []scanCompRef, nComponents )
    for i := uint(0); i < nComponents; i++ {
        sCs[i].cmId = jpg.data[offset]
        eIDs := jpg.data[offset+1]
        sCs[i].dcId = eIDs >> 4
        sCs[i].acId = eIDs & 0x0f
        offset += scanComponentSpecSize
    }

    sc.rstInterval = jpg.nMcuRST
    sc.startSS = jpg.data[offset]
    sc.endSS = jpg.data[offset+1]
    sABP := jpg.data[offset+2]
    sc.sABPh = sABP >> 4
    sc.sABPl = sABP & 0x0f

    // FIXME: check if frame nLines is bigger than a threshold (considered as invalid)
    //        and set it to 0 before calling setScan

    err = jpg.setScan( sc, &sCs );
    return
}

func (jpg *Desc)getEcsFct( frm *frame,
                           s *scan ) (f func ( uint, *scan ) (uint, error), 
                                                                err error) {

    mode := frm.encodingMode()

    switch mode  {
    default:
        err = fmt.Errorf( "processScan: unsupported scanning mode %s in\n",
                          encodingModeString(mode) )
    case BaselineSequential:
        f = jpg.processSequentialEcs
    case ExtendedProgressive:
        if s.startSS == 0 {     // include DC coefficient
            if s.endSS != 0 {
                panic( "Progressive frame mixing DC and AC coefficient in same scan" )
            }
            if s.sABPh == 0 {   // treat initial DC scan as sequential
                f = jpg.processSequentialEcs
            } else {            // special case for refining DC coefficients
                //jpg.Mcu = true  // for debugging
                f = jpg.processRefiningDcEcs
            }
        } else {                // only AC coefficients
            if len( s.sComps ) != 1 {
                panic( "Progressive frame AC only scan with multiple components" )
            }
            if s.sABPh == 0 {   // initial AC scan
                f = jpg.processInitialAcEcs
            } else {
                f = jpg.processRefiningAcEcs
            }
        }
    }
    return
}

func (jpg *Desc) processScan( marker, sLen uint ) error {
//    if jpg.Content { fmt.Printf( "SOS\n" ) }
    if (jpg.state != _SCAN1 && jpg.state != _SCANn) {
        return fmt.Errorf( "processScan: Wrong sequence %s in state %s\n",
                            getJPEGmarkerName(marker), jpg.getJPEGStateName() )
    }
    if sLen < fixedScanHeaderSize {   // fixed size besides components
        return fmt.Errorf( "processScan: Wrong SOS header (len %d)\n", sLen )
    }

    frm := jpg.getCurrentFrame( )
    if frm == nil {
        panic( "Scan without frame" )
    }

    frm.scans = append( frm.scans, scan{ } )    // add new unknown scan
    sc := jpg.getCurrentScan()

    if err := jpg.processScanHeader( sLen, sc ); err != nil {
        return err
    }
    if jpg.state == _SCAN1 {
        jpg.state = _SCAN1_ECS
    } else {
        jpg.state = _SCANn_ECS
    }

    jpg.offset += sLen + 2
    firstECS := jpg.offset

    processECS, err := jpg.getEcsFct( frm, sc )
    if err != nil {
        return err
    }

    rstCount := uint(0)
    var lastRSTIndex, nIx uint
    var lastRST uint = 7
    tLen := uint(len( jpg.data ))   // start hunting for 0xFFxx with xx != 0x00

    var nMCUs uint
    for ; ; {   // processECS return upon error, reached EOF or 0xFF followed by non-zero
        if nMCUs, err = processECS( nMCUs, sc ); err != nil {
            return jpgForwardError( "processScan", err )
        }
        nIx = jpg.offset
        if nIx+1 >= tLen || jpg.data[nIx+1] < 0xd0 || jpg.data[nIx+1] > 0xd7 {
            break
        }       // else one of RST0-7 embedded in scan data, keep going

        if jpg.Warn {
            if jpg.nMcuRST == 0 {
                fmt.Printf( "  WARNING: Restart Marker found without Restart Interval definition\n" )
            } else {
                if nMCUs % jpg.nMcuRST != 0 {
                fmt.Printf( "  WARNING: Restart Marker found before the Restart Interval\n" )
                }
            }
        }

        RST := uint( jpg.data[nIx+1] - 0xd0 )
        if (lastRST + 1) % 8 != RST { // don't try to fix it, as it may indicate
                                      // a corrupted file with missing samples.
            if jpg.Warn {
                fmt.Printf( "  WARNING: invalid RST sequence (%d, expected %d)\n",
                            RST, (lastRST + 1) % 8 )
            }
            // Altough this is highly unlikely, it indicates a gap in encoded
            // samples. Based on the new RST value, calculate how many MCUs
            // have been lost. This is not a fool proof solution since the RST
            // numbers wrap up after 8 and there is no way to know if wrapping
            // occured multiple times. Assuming it did not occur, or only once:
            if jpg.nMcuRST != 0 {                       // fix nMCUs
                nMCUs = (nMCUs / jpg.nMcuRST) * nMCUs   // back to previous
                var delta uint
                if RST > lastRST {
                    delta = RST - lastRST
                } else {
                    delta = 8 - lastRST + RST
                }
                nMCUs += jpg.nMcuRST * delta
            }
        }
        lastRSTIndex = nIx
        lastRST = RST
        rstCount++

        jpg.offset += 2;    // skip RST
    }

    if lastRSTIndex == nIx - 2 {
        if jpg.Warn {
            fmt.Printf( "  WARNING: ending RST is useless\n" )
        }
        if jpg.TidyUp {
            nIx -= 2
            fmt.Printf( "  FIXING: Removing ending RST (useless)\n" )
        }
    }

    sc.ECSs = jpg.data[firstECS:nIx]
    sc.nMcus = nMCUs
    sc.rstCount = rstCount

    jpg.addSeg( sc )
    jpg.state = _SCANn  // accept folloring scans (if progressive mode)

    return nil
}

// ----------------- Restart Intervals

type riSeg struct {
    interval    uint16
}

func (rs *riSeg)serialize( w io.Writer ) (int, error) {
    seg := make( []byte, restartIntervalSize + 2 )
    binary.BigEndian.PutUint16( seg, _DRI )
    binary.BigEndian.PutUint16( seg[2:], restartIntervalSize )
    binary.BigEndian.PutUint16( seg[4:], rs.interval )
    return w.Write( seg )
}

func (rs *riSeg)format( w io.Writer ) (n int, err error) {
    n, err = fmt.Fprintf( w, "  Define Restart Interval:\n    Interval %d MCUs\n",
                          rs.interval )
    if err != nil { err = fmt.Errorf( "format: %w", err ) }
    return
}

func (jpg *Desc)defineRestartInterval( marker, sLen uint ) error {
    offset := jpg.offset + 4
    restartInterval := uint16(jpg.data[offset]) << 8 + uint16(jpg.data[offset+1])

    rs := new( riSeg )
    rs.interval = restartInterval
    jpg.nMcuRST = uint(restartInterval)

    frm := jpg.getCurrentFrame( )
    if frm != nil && frm.resolution.nSamplesLine % restartInterval != 0 {
        if jpg.Warn {
            fmt.Printf( "  Warning: number of samples per line (%d) is not a" +
                        " multiple of the restart interval\n",
                        frm.resolution.nSamplesLine )
        }
    }

    for _, cmp := range frm.components {
        if cmp.nUnitsRow / uint(cmp.HSF) < jpg.nMcuRST {
            fmt.Printf( "  Warning: restart interval %d is larger than the number of MCUs per row\n",
                        jpg.nMcuRST, cmp.nUnitsRow / uint(cmp.HSF) )
            break;
        }
    }

    jpg.addSeg( rs )
    return nil
}

// ------------------ Quantization

type qtSeg struct {
    data    [][65]uint16    // slice of qt arrays (pq,tq)+64entries
}

func (qt *qtSeg)destinations( ) []uint {
    var ds []uint
    for _, v := range qt.data {
        ds = append( ds, uint( v[0] & 0x0f ) )
    }
    return ds
}

func (qt *qtSeg)matchDestination( start int, d uint ) int {
    ds := qt.destinations()
    for i := start; i < len(ds); i++ {
        if ds[i] == d {
            return i
        }
    }
    return -1
}

func (qs *qtSeg)serialize( w io.Writer ) (int, error) {
    n := len(qs.data)
    lq := uint16(2)
    for i := 0; i < n; i++ {
        lq += 65 + ( 64 * ( qs.data[i][0] >> 8) )
    }
    seg := make( []byte, lq + 2 )
    binary.BigEndian.PutUint16( seg[0:], _DQT )
    binary.BigEndian.PutUint16( seg[2:], lq )

    j := 4
    for i := 0; i < n; i++ {
        p := byte(qs.data[i][0] >> 8)
        d := byte(qs.data[i][0])
        seg[j] = (p << 4) | d
        j++
        if p == 0 {
            for k := 1; k < 65; k++ {
                seg[j] = byte(qs.data[i][k])
                j++
            }
        } else {
            for k := 1; k < 65; k++ {
                binary.BigEndian.PutUint16( seg[j:], qs.data[i][k] )
                j += 2
            }
        }
    }
    return w.Write( seg )
}

func formatZigZag( cw *cumulativeWriter, f string, qt *[65]uint16 ) {
    cw.format( "    Zig-Zag: " )
    for i := 1; ;  {
        for j := 0; j < 8; j++ {
            cw.format( f, qt[i+j] )
        }
        i += 8
        if i == 65 { break }
        cw.format(  "\n             " )
    }
    cw.format( "\n" )
}

func formatQuantizationDest( cw *cumulativeWriter,
                             qt *[65]uint16, m FormatMode ) {

    d := qt[0]
    p := ((d >> 4) + 1) << 3
    d &= 0x0f

    cw.format( "  Quantization table: %d\n", d )
    cw.format( "    Precision: %d-bit\n", p )

    var f string
    if p != 8 { f = "%5d " } else { f = "%3d " }

    if m == Standard || m == Both {
        formatZigZag( cw, f, qt )
    }

    if m == Extra || m == Both {
        for i := 0; i < 8; i++ {
            cw.format( "    Row %d: [ ", i )
            for j := 0; j < 8; j++ {
                cw.format( f, qt[1+zigZagRowCol[i][j]] )
            }
            cw.format( "]\n" )
        }
    }
}

func (qs *qtSeg)formatDestAt( cw *cumulativeWriter, index int,
                              mode FormatMode ) {
    if index < 0 || index > len(qs.data) {
        cw.setError( fmt.Errorf( "index %d out of range\n", index ) )
    } else {
        formatQuantizationDest( cw, &qs.data[index], mode )
    }
}

func (qs *qtSeg)formatAllDest( cw *cumulativeWriter, m FormatMode ) {
    for _, qt := range qs.data {
        formatQuantizationDest( cw, &qt, m )
    }
}

func (qs *qtSeg)format( w io.Writer ) (n int, err error) {
    cw := newCumulativeWriter( w )
    for _, qt := range qs.data {
        formatQuantizationDest( cw, &qt, Standard )
    }
    n, err = cw.result()
    if err != nil { err = fmt.Errorf( "format: %w", err ) }
    return
}

func (jpg *Desc)defineQuantizationTable( marker, sLen uint ) ( err error ) {

    end := jpg.offset + 2 + sLen
    offset := jpg.offset + 4
    qtn := int(0)
    qts := new( qtSeg )

    for ; ; { // Mutiple QTs can be combined in a single DQT segment
        pq := uint(jpg.data[offset]) >> 4 // Quantization table element precision
// 0 => 8-bit values; 1 => 16-bit values. Shall be 0 for 8-bit sample precision.
        tq := uint(jpg.data[offset]) & 0x0f // Quantization table destination id
// destination id [0-3] into which the quantization table shall be installed.
        if pq > 1 {
            return fmt.Errorf( "defineQuantizationTable: Wrong precision (%d)\n", pq )
        }
        if tq > 3 {
            return fmt.Errorf( "defineQuantizationTable: Wrong destination (%d)\n", pq )
        }

        qts.data = append( qts.data, [65]uint16{} )
        qts.data[qtn][0] = (uint16(pq) << 8) | uint16(tq)

        offset ++
        jpg.qdefs[tq].size = 8 * (pq+1)
        for i := 0; i < 64; i++ {
            jpg.qdefs[tq].values[i] = uint16(jpg.data[offset])
            offset ++
            if pq != 0 {
                jpg.qdefs[tq].values[i] <<= 8
                jpg.qdefs[tq].values[i] += uint16(jpg.data[offset])
                offset++
            }
            qts.data[qtn][i+1] = jpg.qdefs[tq].values[i]
        }
        qtn++
        if offset >= end {
            break
        }
    }
    if offset != end {
        return fmt.Errorf( "defineQuantizationTable: Invalid DQT length: %d actual: %d\n",
                           sLen, offset - jpg.offset -2 )
    }
    if qtn > 0 {
        jpg.addSeg( qts )
    } else if jpg.Warn {
        fmt.Printf("defineQuantizationTable: Warning: empty segment (ignoring)\n")
    }
    return nil
}

// --------------- huffman tables

func printTree( cw *cumulativeWriter, root *hcnode, indent string ) {
    cw.format(  "%sHuffman codes:\n", indent );

    var buffer  []uint8
    var printNodes func( *hcnode ); printNodes = func( hcn *hcnode ) {
        if hcn.left == nil && hcn.right == nil {
            cw.format( "%s%s: 0x%02x\n", indent, string(buffer), hcn.symbol )
            buffer = buffer[:len(buffer)-1]
        } else {    // right is always present
            buffer = append( buffer, '0' )
            printNodes( hcn.right )
            if hcn.left != nil {
                buffer = append( buffer, '1' )
                printNodes( hcn.left )
            }
            hcn = hcn.parent;
            if hcn != nil {
                buffer = buffer[:len(buffer)-1]
            }
        }
    }
    printNodes( root )
}

func buildTree( values [16][]uint8 ) (root *hcnode) {

    root = new( hcnode )
    var last *hcnode = root
    var level uint

    for i := uint(0); i < 16; i++ {
        cl := i + 1                                     // code length
        for _, symbol := range values[i] {
//            fmt.Printf( "Symbol 0x%02x length %d\n", symbol, cl )
            for ; level < cl; {
                if nil == last.right {
//                    fmt.Printf( "level %d Last node %p .right is nil\n", level, last  )
                    last.right = new( hcnode)
                    last.right.parent = last
                    last = last.right
                    level++
                } else if nil == last.left {
//                    fmt.Printf( "level %d Last node %p .left is nil\n", level, last )
                    last.left = new( hcnode )
                    last.left.parent = last
                    last = last.left
                    level++
                } else {
//                    fmt.Printf( "level %d Last node %p .left & right are not nil, back up\n", level, last  )
                    if level == 0 {
                        panic( fmt.Sprintf( "backing up above root: code length %d, level %d last %p, root %p\n",
                                            cl, level, last, root ) )
                    }
                    last = last.parent
                    level--
                }
            }

//            fmt.Printf( "level %d, last node %p\n", level, last )
            // last is a new leaf
            if last.left != nil || last.right != nil {
                panic( fmt.Sprintf( "level %d Last node %p is not a leaf node", level, last ) )
            }
//            fmt.Printf( "Make node for symbols 0x%02x\n", symbol )
            last.symbol = symbol
            last = last.parent
            level--
        }
    }
    return
}

type htcd struct {
    data    [16][]uint8 // table data
    hc      byte        // class [0-1]
    hd      byte        // destination [0-3]
}

type htSeg struct {
    htcds   []htcd
}

func (hs *htSeg)serialize( w io.Writer ) (int, error) {
    lh := uint16(2)
    for i := 0; i < len(hs.htcds); i++ {
        sz := 17
        for j := 0; j < 16; j++ {
            sz += len(hs.htcds[i].data[j])
        }
//        fmt.Printf( "nb values for table %d class %d dest %d is %d\n",
//                    i, hs.htcds[i].hc, hs.htcds[i].hd, sz )
        lh += uint16(sz)
    }
//    fmt.Printf( "Total huffman segment length = %d\n", lh )

    seg := make( []byte, lh + 2 )
    binary.BigEndian.PutUint16( seg[0:], _DHT )
    binary.BigEndian.PutUint16( seg[2:], lh )
    j := 4
    for i := 0; i < len(hs.htcds); i++ {
        seg[j] = (hs.htcds[i].hc << 4) | hs.htcds[i].hd
        j++
//        sz := 0
        for k := 0; k < 16; k++ {
//            sz += len(hs.htcds[i].data[k])
            seg[j] = byte( len(hs.htcds[i].data[k]) )
            j++
        }
        for k := 0; k < 16; k++ {
            copy( seg[j:], hs.htcds[i].data[k] )
            j+= len( hs.htcds[i].data[k] )
        }
    }
    return w.Write( seg )
}

type classDestination struct {
    hc      byte        // class [0-1]
    hd      byte        // destination [0-3]
}

func (hs *htSeg)classDestinations( ) []classDestination {
    var cds []classDestination
    for _, v := range hs.htcds {
        cds = append( cds, classDestination{ v.hc, v.hd } )
    }
    return cds
}

func (hs *htSeg)matchClassDestination( start int, c, d byte ) int {
    cds := hs.classDestinations()
    // c: 0=DC 1=AC d: 0-3
    for i := start; i < len(cds); i++ {
        if cds[i].hc == c && cds[i].hd == d {
            return i
        }
    }
    return -1
}

func formatHuffmanDest( cw *cumulativeWriter, ht *htcd, mode FormatMode ) {

    var class string
    if ht.hc == 0 {
        class = "DC"
    } else {
        class = "AC"
    }

    cw.format( "  Huffman table %s%d\n", class, ht.hd )
    if mode == Standard || mode == Both {
        cw.format( "    Code lengths and symbols:\n" )

        var nSymbols uint
        for i := 0; i < 16; i++ {
            li := uint( len(ht.data[i]) )
            if li == 0 {
                continue
            }
            nSymbols += li
            cw.format( "    length %2d: %3d symbols: [ ", i+1, li )

    VALUE_LOOP:
            for j := uint(0); ;  {
                for k := uint(0); k < 8; k++ {
                    if j+k >= li {
                        break VALUE_LOOP
                    }
                    cw.format( "0x%02x ", ht.data[i][j+k] )
                }
                cw.format(  "\n                              ")
                j += 8
            }
            cw.format( "]\n" )
        }
        cw.format( "    Total number of symbols: %d\n", nSymbols )
    }
    if mode == Extra || mode == Both {
        root := buildTree( ht.data )
        printTree( cw, root, "    " )
    }
    return
}

func (hs *htSeg)formatDestAt( cw *cumulativeWriter, index int,
                              mode FormatMode ) {
    if index < 0 || index > len(hs.htcds) {
        cw.setError( fmt.Errorf( "index %d out of range\n", index ) )
    } else {
        formatHuffmanDest( cw, &hs.htcds[index], mode )
    }
}

func (hs *htSeg)formatAllDest( cw *cumulativeWriter, m FormatMode ) {
    for _, ht := range hs.htcds {
        formatHuffmanDest( cw, &ht, m )
    }
}

func (hs *htSeg)format( w io.Writer ) (n int, err error) {
    cw := newCumulativeWriter( w )
    for _, ht := range hs.htcds {
        formatHuffmanDest( cw, &ht, Standard )
    }
    n, err = cw.result()
    if err != nil { err = fmt.Errorf( "format: %w", err ) }
    return
}

func (jpg *Desc)defineHuffmanTable( marker, sLen uint ) ( err error ) {

    end := jpg.offset + 2 + sLen
    offset := jpg.offset + 4

    hts := new( htSeg )
    ht := 0
    for ; ; {
        tc := uint(jpg.data[offset]) >> 4
        th := uint(jpg.data[offset]) & 0x0f
        offset++

//        fmt.Printf("defineHuffmanTable: class %d dest %d\n", tc, th )
        if tc > 1 || th > 3 {
            return fmt.Errorf( "defineHuffmanTable: Wrong table class/destination (%d/%d)\n", tc, th )
        }

        hts.htcds = append( hts.htcds, htcd{ } )
        hts.htcds[ht].hc = byte(tc)
        hts.htcds[ht].hd = byte(th)

        td := 2*th+tc // use 8 tables, (1 for DC + 1 for AC per destination) * 4
        voffset := offset+16

        for hcli := uint(0); hcli < 16; hcli++ {
            jpg.hdefs[td].values[hcli] = nil    // ready to replace table (append)
        }
        for hcli := uint(0); hcli < 16; hcli++ {
            li := uint(jpg.data[offset+hcli])
            jpg.hdefs[td].values[hcli] = append(
                   jpg.hdefs[td].values[hcli], jpg.data[voffset:voffset+li]... )
            // since another definition can replace data at destination, a copy
            // is necessary here in order to keep the original definition.
            hts.htcds[ht].data[hcli] = make( []byte, li )
            copy( hts.htcds[ht].data[hcli], jpg.hdefs[td].values[hcli] )
            voffset += li
        }
        jpg.hdefs[td].root = buildTree( jpg.hdefs[td].values )
        fmt.Printf("Huffman table class %d dest %d defined\n", tc, th )

        ht++
        offset = voffset;
        if offset >= end {
            break
        }
    }
    if offset != end {
        return fmt.Errorf( "defineHuffmanTable: Invalid DHT length: %d actual: %d\n",
                           sLen, offset - jpg.offset -2 )
    }
    if ht > 0 {
        jpg.addSeg( hts )
    } else if jpg.Warn {
        fmt.Printf("defineHuffmanTable: Warning: empty segment (ignoring)\n")
    }
    return nil
}

// -------------- comment segment

type comSeg struct {
    text    []byte
}

func (c *comSeg)serialize( w io.Writer ) (int, error) {
    size  := fixedCommentHeaderSize + uint16( len(c.text) )
    seg := make( []byte, size + 2 )
    binary.BigEndian.PutUint16( seg, _COM )
    binary.BigEndian.PutUint16( seg[2:], size )
    copy(seg[4:], c.text )
    return w.Write( seg )
}

func (c *comSeg)format( w io.Writer ) (n int, err error) {
    n, err = fmt.Fprintf( w, "Comment:\n  \"%s\"\n",
                          string(c.text) )
    if err != nil { err = fmt.Errorf( "format: %w", err ) }
    return
}

func (jpg *Desc)commentSegment( marker, sLen uint ) error {
    offset := jpg.offset
    var b bytes.Buffer
    s := jpg.data[offset:offset+sLen]
    b.Write( s )
    c := new(comSeg)
    c.text = b.Bytes()
    jpg.addSeg( c )
    return nil
}

// ----------------- define number of lines

type dnlSeg struct {
    nLines  uint16
    toRemove bool
}

func (d *dnlSeg)serialize( w io.Writer ) (int, error) {
    if d.toRemove {
        return 0, nil
    }
    seg := make( []byte, defineNumberOfLinesSize + 2 )
    binary.BigEndian.PutUint16( seg, _DNL )
    binary.BigEndian.PutUint16( seg[2:], defineNumberOfLinesSize )
    binary.BigEndian.PutUint16( seg[4:], d.nLines )
    return w.Write( seg )
}

func (d *dnlSeg)format( w io.Writer ) (n int, err error) {
    n, err = fmt.Fprintf( w, "Define Number Of Lines:\n  number of lines %d\n",
                          d.nLines )
    return
}

func (jpg *Desc)defineNumberOfLines( marker, sLen uint ) ( err error ) {
    if jpg.state != _SCANn {
        return fmt.Errorf( "defineNumberOfLines: Wrong sequence %s in state %s\n",
                       getJPEGmarkerName(marker), jpg.getJPEGStateName() )
    }
    if sLen != 4 {   // fixed size
        return fmt.Errorf( "defineNumberOfLines: Wrong DNL header (len %d)\n", sLen )
    }
    cf := jpg.getCurrentFrame()
    if cf == nil { panic("defineNumberOfLines: no current frame\n") }
    if cf.resolution.dnlLines != 0 {
        return fmt.Errorf( "defineNumberOfLines: Multiple DNL tables\n" )
    }

    offset := jpg.offset + 4
    nLines := uint16(jpg.data[offset]) << 8 + uint16(jpg.data[offset+1])
    cf.resolution.dnlLines = nLines
    var toRemove bool
    if ( cf.resolution.nLines != 0 ) {
        if jpg.Warn {
            fmt.Printf( "Warning: DNL table found with non 0 SOF number " +
                        "of lines (%d)\n", cf.resolution.nLines )
        }
        if jpg.TidyUp {
            toRemove = true
        }
    }
    nls := new( dnlSeg )
    nls.nLines = nLines
    nls.toRemove = toRemove
    jpg.addSeg( nls )
    return
}

func (jpg *Desc)fixLines( ) {

    frm := jpg.getCurrentFrame( )
    if frm.encoding > HuffmanExtendedSequential {
        fmt.Printf("Non Sequential Huffman coded frame(s): lines are left untouched\n")
        return
    }
    // use actual number of unit rows from Y component
    nLines := len(frm.components[0].iDCTdata)   // nUnits Y Col
    yVSF := int(frm.components[0].VSF)          // nUnits per MCU col
    for _, cmp := range frm.components {
        if (len(frm.components[0].iDCTdata) * yVSF) / int(cmp.VSF) != nLines {
            panic( "Inconsistent frame component resolution\n" )
        }
    }
    scanLines := uint16(nLines * 8)             // 8 pixel lines per unit
    if scanLines < frm.resolution.nLines ||
        scanLines > (frm.resolution.nLines - (uint16(frm.resolution.mvSF) * 8)) {
        fmt.Printf( "  FIXING: replacing number of lines in Start Of Frame " +
                    "with actual scan results (from %d to %d)\n",
                    frm.resolution.nLines, scanLines )
        frm.resolution.scanLines = scanLines
    }
}
