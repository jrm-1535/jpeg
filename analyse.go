// Package jpeg provides a few primitives to parse and analyse a JPEG image
package jpeg

import (
    "fmt"
    "bytes"
    "os"
    "io"
    "io/ioutil"
)

/* JPEG document structure:
Must start with SOI, contain a single frame and end with EOI: 0xffd8 ...... 0xffd9
   SOI <frame> EOI
      Start Of Image (SOI): 0xffd8, End Of Image (EOI): 0xffd9
A frame can be made of multiple scans:
   may start with optional tables,
   followed by one mandatory frame header,
   followed by one scan segment,
   optionally followed by a number of lines segment (DNL): 0xffdc
   optionally followed by multiple other scan segments, each without a DNL
   [optional tables]<frame header><scan segment #1>[DNL][<scan segment #2>...<last scan segment>
Optional tables may appear immediately after SOI or immediately after frame header SOFn. They are:
   Application data (APP0 to APP15) 1 APP required: APP0 for JFIF,
   Quantization Table (DQT), at least 1 required
   Huffman Table (DHT) required if SOF1, SOF2, SOF3, SOF5, SOD6 or SOF7,
   Arithmetic Coding Table (DAC) required if SOF9, SOF10, SOF11, SOF13, SOD14 or SOF15, and default table not used,
   Define Restart Interval (DRI) required if RSTn markers are used.
   Hierarchical Progression Table (DHP) (?)
   Comment
A Frame header is a start of frame (SOFn): 0xffCn, where n is from 0 to 15 minus multiple of 4
   Each SOFn implies the following encoded scan data format, according to the n in SOFn
   All SOFn segments share the same syntax:
   SOfn, 2 byte size, 1 byte sample precision, 2 byte number of lines, 2 byte number of samples/line,
                      1 byte number of following components, for each of those components:
                         1 byte unique component id,
                         4 bit  horizontal sampling factor (number of component H units in each MCU)
                         4 bit  vertical sampling factor (number of component V units in each MCU)
                         2 byte quantization table selector
A DNL segment is 0xffdc 0x0002 0xnnnn where nnnn is the number of lines in the immediately preceding SOF
A scan segment 
   may start with optional tables
   followed by one mandatory scan header
   followed by one entropy-coded segment (ECS)
   followed by multiple sequences of one RSTn (Restart) and one ECS (only if restart is enabled)
        RSTn indicates one restart interval from RST0 to RST7, starting from 0 and incrementing before wrapping around
A scan header segment is start of scan (SOS) segment: 0xffda with the following synrax
   SOS, 2 byte size, 1 byte number of components in scan, for each of those components:
                         1 byte component selector (must match one of unique component ids in frame header)
                         4 bit  DC entropy coding table selector
                         4 bit  AC entropy coding table selector
                     1 byte start of spectral or predictor selectopn
                     1 byte end of spectral or predictor selectopn
                     4 bit successive approximation bit position high
                     4 bit successive approximation bit position low
An ECS segment is made of multiple <MCUs> each ECS with the same RI (Restart Interval) MCUs, except the last one
A Quantization Table (DQT) starts with 0xffdb, followed by 2 byte segment length,
    followed by a number n of tables: n = ( segment - len ) /  ( 65 + 64 * precisiom ), each table is:
      4  bit quantization element precision (0 =>8 bits, 1 => 16 bits)
         4 bit destination ID (0-3) referred to by start of frame quantization table selector
         1 or 2 byte quantization table element (according to the precision) * 64 elements

Example:
   SOI [frame loop tables] SOFn [ scan loop tables ] SOS scan1 data [ DNL] scan 2 data ... scan last data EOI
*/

const (                         // JPEG parsing state
    _INIT = iota                // expecting SOI
    _APPLICATION                // from _INIT after SOI, expecting APP0 and APP0 ext
    _FRAME                      // from _APP after any table other than APP0
    _SCAN1                      // from _FRAME after SOFn, expecting DHT, DAC, DQT, DRI, COM, or SOS
    _SCAN1_ECS                  // from _SCAN1 after SOS, expecting ECSn/RStn, DHT, DAC, DQT, DRI, COM, SOS, DNL or EOI
    _SCANn                      // from _SCAN1_ECS, after DNL, expecting DHT, DAC, DQT, DRI, COM, SOS or EOI
    _SCANn_ECS                  // from _SCANn, after SOS, expecting ECSn/RStn, DHT, DAC, DQT, DRI, COM, SOS or EOI
    _FINAL                      // from either _SCAN1_ECS or _SCANn_ECS, after EOI
)

/* State transitions
 _INIT        -> _APPLICATION   transition on SOI
 _APPLICATION -> _FRAME         transition on any table other than APP0
 _FRAME       -> _SCAN1         transition on SOFn
 _SCAN1       -> _SCAN1_ECS     transition on SOS
 _SCAN1_ECS   -> _FINAL         transition on EOI
 _SCAN1_ECS   -> _SCANn         transition on DNL
 _SCANn       -> _SCANn_ECS     transition on SOS
 _SCANn_ECS   -> _FINAL         transition on EOI
*/

func (jpg *JpegDesc) getJPEGStateName( ) string {
    if jpg.state > _FINAL { return "Unknown state" }

    names := [...]string {
        "initial", "application", "frame",
        "first scan", "first scan encoded segment",
        "other scan", "other scan encoded segment",
        "final" }
    return names[ jpg.state ]
}

type source uint                // whether segment is from raw data or has been modified after fixing

const (
    original source = iota      // source is jpg.data
    modified                    // source is jpg.update
)

type segment struct {           // one for each table, scan or group of scans
    from            source      // what source for start and stop indexes
    start, stop     uint        // offsets where to start and stop segment
}

type iDCTRow        [][64]int   // dequantizised iDCT matrices (yet to inverse)

type scanComp struct {
    hDC, hAC        *hcnode     // huffman roots for DC and AC coefficients
                                // use hDC for 1st sample, hAC for all others
    dUnits          [][64]int   // up to vSF rows of hSF data units (64 int)
    iDCTdata        []iDCTRow   // rows of reordered iDCT matrices
    previousDC      int         // previous DC value for this component
    nUnitsRow       uint        // n units per row = nSamplesLines/8
    hSF, vSF        uint        // horizontal & vertical sampling factors
    dUCol           uint        // increments with each dUI till it reaches hSF
    dURow           uint        // increments with each row till it reaches vSF
    dUAnchor        uint        // top-left corner of dUnits area, incremented
                                // by hSF each time hSF*vSF data units are done
    nRows           uint        // number of rows already processed
    count           uint8       // current sample count [0-63] in each data unit
}

type mcuDesc struct {           // Minimum Coded Unit Descriptor
    sComps           []scanComp // one per scan component in order: Y, [Cb, Cr]
}

type scan   struct {            // one for each scan
    tables          []segment   // scan tables in file order terminated by 1 SOS
    ECSs            []segment   // entropy coded segments constituting the scan
    mcuD            *mcuDesc    // MCU definition for the scan
    nMcus           uint        // total number of MCUs in scan
}

type qdef struct {
    precision       bool        // true for 16-bit precision, false for 8-bit
    values          [64]uint16  // actually often uint8, but may be uint16
}

type hcnode struct {
    left, right     *hcnode
    parent          *hcnode
    symbol          uint8
}

type hcdef struct {
    length          uint
    values          []uint8
}

type hdef struct {
    cdefs           [16]hcdef
    root            *hcnode
}

type component struct {
    id, hSF, vSF, qS uint       // IDs for component & comp quantization table
}

type sampling  struct {
    samplePrecision uint        // number of bits per sample
    nLines          uint        // number of lines
    nSamplesLine    uint        // number of samples per line
    mhSF, mvSF      uint        // max horizontal and vertical sampling factors
}

type control struct {
                    Control
}

// JpegDesc is the internal structure describing the JPEG file
type JpegDesc struct {
    data            []byte      // raw data file
    update          []byte      // modified data (only if fix is true and issues are encountered)

    offset          uint        // current offset in data
    state           int         // INIT, IMAGE, FRAME, SCAN1, REMAINING, FINAL

    app0Extension   bool        // APP0 followed by APP0 extension
    pDNL            bool        // DNL table processed
    gDNLnLines      uint        // DNL given nLines in picture
    nMcuRST         uint        // number of MCUs expected between RSTn

    qdefs           [4]qdef     // Quantization zig-zag coefficients for 4 destinations
    hdefs           [8]hdef     // Huffman code definition for 4 destinations * (DC+following AC)

    tables          []segment   // frame tables: APP0(s) followed by optional tables and 1 terminating SOFn
    components      []component // from SOFn component definitions
                                // note: component order is Y [, Cb, Cr] in SOFn
    resolution      sampling    // luminance (greyscale) or YCbCr picture sampling resolution
    scans           []scan      // for the scans following SOFn

                    control     // what to print/fix during analysis
}

const (                 // JPEG Marker Definitions

    _TEM   = 0xff01     // Temporary use in arithmetic coding

    _SOF0  = 0xffC0     // Start Of Frame Huffman-coding frames (Baseline DCT)
    _SOF1  = 0xffc1     // Start Of Frame Huffman-coding frames (Extended Sequential DCT)
    _SOF2  = 0xffc2     // Start Of Frame Huffman-coding frames (Progressive DCT)
    _SOF3  = 0xffc3     // Start Of Frame Huffman-coding frames (Lossless / sequential)
    _DHT   = 0xffc4     // Define Huffman Table
    _SOF5  = 0xffc5     // Start Of Frame Differential Huffman-coding frames (Sequential DCT)
    _SOF6  = 0xffc6     // Start Of Frame Differential Huffman-coding frames (Progressive DCT)
    _SOF7  = 0xffc7     // Start Of Frame Differential Huffman-coding frames (Lossless0
    _JPG   = 0xffc8     // Reserved for JPEG extensions
    _SOF9  = 0xffc9     // Start Of Frame Arithmetic-coding FRames (Extended sequential DCT)
    _SOF10 = 0xffca     // Start Of Frame Arithmetic-coding FRames (Progressive DCT)
    _SOF11 = 0xffcb     // Start Of Frame Arithmetic-coding FRames (Lossless / sequential)
    _DAC   = 0xffcc     // Define Arithmetic Coding Table
    _SOF13 = 0xffcd     // Start Of Frame Differential Arithmetic-coding FRames (Sequential DCT)
    _SOF14 = 0xffce     // Start Of Frame Differential Arithmetic-coding FRames (Progressive DCT)
    _SOF15 = 0xffcf     // Start Of Frame Differential Arithmetic-coding FRames (Lossless)

    _RST0  = 0xffd0     // ReStarT #0
    _RST1  = 0xffd1     // ReStarT #1
    _RST2  = 0xffd2     // ReStarT #2
    _RST3  = 0xffd3     // ReStarT #3
    _RST4  = 0xffd4     // ReStarT #4
    _RST5  = 0xffd5     // ReStarT #5
    _RST6  = 0xffd6     // ReStarT #6
    _RST7  = 0xffd7     // ReStarT #7
    _SOI   = 0xffd8     // Start Of Image
    _EOI   = 0xffd9     // End Of Image
    _SOS   = 0xffda     // Start Of Scan
    _DQT   = 0xffdb     // Define Quantization Table
    _DNL   = 0xffdc     // Define Number of lines
    _DRI   = 0xffdd     // Define Reset Interval
    _DHP   = 0xffde     // Define Hierarchical Progression
    _EXP   = 0xffdf     // Expand reference image

    _APP0  = 0xffe0     // Application Vendor Specific #0 (JFIF)
    _APP1  = 0xffe1     // Application Vendor Specific #1 (EXIF, TIFF, DCF, TIFF/EP, Adobe XMP)
    _APP2  = 0xffe2     // Application Vendor Specific #2 (ICC)
    _APP3  = 0xffe3     // Application Vendor Specific #3 (META)
    _APP4  = 0xffe4     // Application Vendor Specific #4
    _APP5  = 0xffe5     // Application Vendor Specific #5
    _APP6  = 0xffe6     // Application Vendor Specific #6
    _APP7  = 0xffe7     // Application Vendor Specific #7
    _APP8  = 0xffe8     // Application Vendor Specific #8
    _APP9  = 0xffe9     // Application Vendor Specific #9
    _APP10 = 0xffea     // Application Vendor Specific #10
    _APP11 = 0xffeb     // Application Vendor Specific #11
    _APP12 = 0xffec     // Application Vendor Specific #12 (Picture Info, Ducky)
    _APP13 = 0xffed     // Application Vendor Specific #13 (Photoshop Adobe IRB)
    _APP14 = 0xffee     // Application Vendor Specific #14 (Adobe)
    _APP15 = 0xffef     // Application Vendor Specific #15

    _RES0  = 0xfff0     // Reserved for JPEG extensions #0
    _RES1  = 0xfff1     // Reserved for JPEG extensions #1
    _RES2  = 0xfff2     // Reserved for JPEG extensions #2
    _RES3  = 0xfff3     // Reserved for JPEG extensions #3
    _RES4  = 0xfff4     // Reserved for JPEG extensions #4
    _RES5  = 0xfff5     // Reserved for JPEG extensions #5
    _RES6  = 0xfff6     // Reserved for JPEG extensions #6
    _RES7  = 0xfff7     // Reserved for JPEG extensions #7
    _RES8  = 0xfff8     // Reserved for JPEG extensions #8
    _RES9  = 0xfff9     // Reserved for JPEG extensions #9
    _RES10 = 0xfffa     // Reserved for JPEG extensions #10
    _RES11 = 0xfffb     // Reserved for JPEG extensions #11
    _RES12 = 0xfffc     // Reserved for JPEG extensions #12
    _RES13 = 0xfffd     // Reserved for JPEG extensions #13

    _COM   = 0xfffe     // Comment (text)
)

func getJPEGTagName( tag uint ) string {
    if tag == _TEM { return "TEM Temporary use in arithmetic coding" }
    if tag < _SOF0 || tag > _COM { return "RES Reserved Marker" }

    names := [...]string {
        "SOF0 Start Of Frame Huffman-coding frames (Baseline DCT)",
        "SOF1 Start Of Frame Huffman-coding frames (Extended Sequential DCT)",
        "SOF2 Start Of Frame Huffman-coding frames (Progressive DCT)",
        "SOF3 Start Of Frame Huffman-coding frames (Lossless / sequential)",
        "DHT Define Huffman Table",
        "SOF5 Start Of Frame Differential Huffman-coding frames (Sequential DCT)",
        "SOF6 Start Of Frame Differential Huffman-coding frames (Progressive DCT)",
        "SOF7 Start Of Frame Differential Huffman-coding frames (Lossless0",
        "JPG Reserved for JPEG extensions",
        "SOF9 Start Of Frame Arithmetic-coding FRames (Extended sequential DCT)",
        "SOF10 Start Of Frame Arithmetic-coding FRames (Progressive DCT)",
        "SOF11 Start Of Frame Arithmetic-coding FRames (Lossless / sequential)",
        "DAC Define Arithmetic Coding Table",
        "SOF13 Start Of Frame Differential Arithmetic-coding FRames (Sequential DCT)",
        "SOF14 Start Of Frame Differential Arithmetic-coding FRames (Progressive DCT)",
        "SOF15 Start Of Frame Differential Arithmetic-coding FRames (Lossless)",

        "RST0 ReStarT #0",
        "RST1 ReStarT #1",
        "RST2 ReStarT #2",
        "RST3 ReStarT #3",
        "RST4 ReStarT #4",
        "RST5 ReStarT #5",
        "RST6 ReStarT #6",
        "RST7 ReStarT #7",
        "SOI Start Of Image",
        "EOI End Of Image",
        "SOS Start Of Scan",
        "DQT Define Quantization Table",
        "DNL Define Number of lines",
        "DRI Define Reset Interval",
        "DHP Define Hierarchical Progression",
        "EXP Expand reference image",

        "APP0 Application Vendor Specific #0 (JFIF)",
        "APP1 Application Vendor Specific #1 (EXIF, TIFF, DCF, TIFF/EP, Adobe XMP)",
        "APP2 Application Vendor Specific #2 (ICC)",
        "APP3 Application Vendor Specific #3 (META)",
        "APP4 Application Vendor Specific #4",
        "APP5 Application Vendor Specific #5",
        "APP6 Application Vendor Specific #6",
        "APP7 Application Vendor Specific #7",
        "APP8 Application Vendor Specific #8",
        "APP9 Application Vendor Specific #9",
        "APP10 Application Vendor Specific #10",
        "APP11 Application Vendor Specific #11",
        "APP12 Application Vendor Specific #12 (Picture Info, Ducky)",
        "APP13 Application Vendor Specific #13 (Photoshop Adobe IRB)",
        "APP14 Application Vendor Specific #14 (Adobe)",

        "RES0 Reserved for JPEG extensions #0",
        "RES1 Reserved for JPEG extensions #1",
        "RES2 Reserved for JPEG extensions #2",
        "RES3 Reserved for JPEG extensions #3",
        "RES4 Reserved for JPEG extensions #4",
        "RES5 Reserved for JPEG extensions #5",
        "RES6 Reserved for JPEG extensions #6",
        "RES7 Reserved for JPEG extensions #7",
        "RES8 Reserved for JPEG extensions #8",
        "RES9 Reserved for JPEG extensions #9",
        "RES10 Reserved for JPEG extensions #10",
        "RES11 Reserved for JPEG extensions #11",
        "RES12 Reserved for JPEG extensions #12",
        "RES13 Reserved for JPEG extensions #13",

        "COM Comment",
  }

    return names[ tag - _SOF0 ]
}

func jpgForwardError( prefix string, err error ) error {
    return fmt.Errorf( prefix + ": %v", err )
}

func (jpg *JpegDesc) getLastGlobalTable() *segment {
    l := len( jpg.tables )
    if l > 0 {
        return &jpg.tables[l - 1]
    }
    return nil
}

func (jpg *JpegDesc) getCurrentScan() *scan {
    l := len( jpg.scans )
    if l > 0 {
        return &jpg.scans[l - 1]
    }
    return nil
}

func (jpg *JpegDesc)addECS( start, stop uint, from source, nMcus uint ) error {
    if jpg.state != _SCAN1_ECS && jpg.state != _SCANn_ECS {
        return fmt.Errorf( "addECS: Wrong state %s for ECS\n", jpg.getJPEGStateName() )
    }
    scan := jpg.getCurrentScan()
    if scan == nil || scan.tables == nil {  // at least SOS in scan.tables
        return fmt.Errorf( "addECS: Wrong scan data (%v)\n", *scan )
    }
    scan.nMcus = nMcus      // store total number of MCUs in scan
    scan.ECSs = append( scan.ECSs, segment{ from: from, start: start, stop: stop } )
    return nil
}

func (jpg *JpegDesc)addTable( tag, start, stop uint, from source ) error {
    table := segment{ from: from, start: start, stop: stop }
    if jpg.state == _APPLICATION || jpg.state == _FRAME {
        jpg.tables = append( jpg.tables, table )
    } else if jpg.state == _SCAN1 || jpg.state == _SCANn {
        scan := jpg.getCurrentScan()
        scan.tables = append( scan.tables, table )
    } else {
        return fmt.Errorf( "addTable: Wrong sequence %s in state %s\n",
                           getJPEGTagName(tag), jpg.getJPEGStateName() )
    }
    return nil
}

const (                             // Image resolution units (prefixed with _ to avoid being documented)
    _DOTS_PER_ARBITRARY_UNIT = 0    // undefined unit
    _DOTS_PER_INCH = 1              // DPI
    _DOTS_PER_CM = 2                // DPCM Dots per centimeter
)

func getUnitsString( units int ) (string, string) {
    switch units {
    case _DOTS_PER_ARBITRARY_UNIT: return "dots per abitrary unit", "dp?"
    case _DOTS_PER_INCH:           return "dots per inch", "dpi"
    case _DOTS_PER_CM:             return "dots per centimeter", "dpcm"
    }
    return "Unknown units", ""
}

func isTagSOFn( tag uint ) bool {
    if tag < _SOF0 || tag > _SOF15 { return false }
    if tag == _DHT || tag == _JPG || tag == _DAC { return false }
    return true
}

const (
    _APP0_JFIF = iota
    _APP0_JFXX
)

func markerAPP0discriminator( h5 []byte ) int {
    if bytes.Equal( h5, []byte( "JFIF\x00" ) ) { return _APP0_JFIF }
    if bytes.Equal( h5, []byte( "JFXX\x00" ) ) { return _APP0_JFXX }
    return -1
}

const (
    _THUMBNAIL_BASELINE = 0x10
    _THUMBNAIL_PALETTE  = 0x11
    _THUMBNAIL_RGB      = 0x12
)

func (jpg *JpegDesc) app0( tag, sLen uint ) error {
    if sLen < 8 {
        return fmt.Errorf( "app0: Wrong APP0 (JFIF) header (invalid length %d)\n", sLen )
    }
    if jpg.state != _APPLICATION {
        return fmt.Errorf( "app0: Wrong sequence %s in state %s\n",
                           getJPEGTagName(_APP0), jpg.getJPEGStateName() )
    }
    offset := jpg.offset + 4    // points 1 byte after length
    appType := markerAPP0discriminator( jpg.data[offset:offset+5] )
    if appType == -1 {
        return fmt.Errorf( "app0: Wrong APP0 header (%s)\n", jpg.data[offset:offset+4] )
    }

    if jpg.Content {
        fmt.Printf( "APP0\n" )
    }
    var err error
    if appType == _APP0_JFIF {
        if sLen < 16 {
            return fmt.Errorf( "app0: Wrong APP0 (JFIF) header (invalid length %d)\n", sLen )
        }

        HtNail := uint( jpg.data[offset+12] )
        VtNail := uint( jpg.data[offset+13] )
        if jpg.Content {
            major := uint( jpg.data[offset+5] )  // 0x01
            minor := uint( jpg.data[offset+6] )  // 0x02
            fmt.Printf( "  JFIF Version %d.%02d\n", major, minor )

            unitCode := int( jpg.data[offset+7] )
            units, symb := getUnitsString( unitCode )
            fmt.Printf( "  size in %s (%s)\n", units, symb )

            Hdensity := uint( jpg.data[offset+8] ) << 8 + uint( jpg.data[offset+9] )
            Vdensity := uint( jpg.data[offset+10] ) << 8 + uint( jpg.data[offset+11] )
            fmt.Printf( "  density %d,%d %s\n", Hdensity, Vdensity, symb )
            fmt.Printf( "  thumbnail %d,%d pixels\n", HtNail, VtNail )
        }
        if sLen != 16 + HtNail * VtNail {
            return fmt.Errorf( "app0: Wrong APP0 (JFIF) header (len %d)\n", sLen )
        }

        err = jpg.addTable( tag, jpg.offset, jpg.offset + 2 + sLen, original )

    } else {
        if len(jpg.tables) != 1 {
            return fmt.Errorf( "app0: APP0 extension does not follow APP0 (JFIF)\n" )
        }
        if jpg.app0Extension {
            return fmt.Errorf( "app0: Multiple APP0 extensions\n" )
        }
        if jpg.Content {
            fmt.Printf( "  JFIF extension\n" )
            extCode := uint( jpg.data[offset+5] )
            switch extCode {
            default:
                return fmt.Errorf( "app0: Wrong JFIF extention code (thumbnail) (code 0x%02d)\n", extCode )
            case _THUMBNAIL_BASELINE:    // ignore for now
                fmt.Printf( "  Thumbnail encoded according to ITU-T T.81 | ISO/IEC 10918-1 baseline process\n" )
            case _THUMBNAIL_PALETTE:     // ignore for now
                fmt.Printf( "  Thumbnail encoded as 1 byte per pixel in 256 entry RGB palette\n" )
            case _THUMBNAIL_RGB:         // ignore for now
                fmt.Printf( "  Thumbnail encoded as RGB (3 bytes per pixel)\n" )
            }
        }
        jpg.app0Extension = true
        err = jpg.addTable( tag, jpg.offset, jpg.offset + 2 + sLen, original )
    }
    if err != nil { return jpgForwardError( "app0", err ) }
    return nil
}

type scanCompRef struct {      // scan component reference
    CMId, DCId, ACId uint
}

/*
    MCU is Minimum Coded Unit

    If the image is grayscale, MCU is just one data unit (8*8 samples)
    if the image is Luminance Y and 2 Chrominance (Cb, Cr) values, MCU may
    be a series of Y, Cb, Cr data units in case of a single interleaved
    scan, or just a single data unit in case of a several separate scans
    of non-interleaved data units.

    In case of interleaved data units, MCU gives the number of data units 
    included in the MCU and for each data unit the relative location of the
    data unit in the complete image.

    For example, in the typical case of 4, 2, 0 chroma subsampling, the first
    4 data units are LUMA (the first in the current luma anchor location, the
    second in the current luma anchor location + 1 on the same row, the third
    in the current luma anchor location on the row below and the fourth in the
    current luma anchor locatiopn + 1 on the row below), the fifth is chroma
    Cb (in the current Cb amchor location) and the sixth is the chroma Cr (in
    the current Cb anchor location). Once a MCU is completed, luma anchor
    location in incremented by 2 in both column and row, whereas both Cb and Cr
    anchor locations are just incremented in column, until the end of their row.

    The end of row is the number of data units per line, that is:
        samples/line/sizeof(dataUnitRow) = samples/lines/8

    However, sometimes the value of samples/line given in the SOF header is not
    aligned with the restart marker intervals, if restart markers are used. In
    case of disagreement, the number of data units in a row is aligned on the
    restart interval in order to make enough room for all data units in a scan
    segment (between 2 restart intervals).

    In that case the end of row is the number of MCUs between 2 restart markers
    (restart interval) multiplied by the number of data units in the MCU, on a
    component per component basis.

        nMcuRST * component.hSF

    McuDesc drives decompression by providing the huffman tables for each
    data unit in the MCU, and for each data unit the location of each decoded
    sample:

    hDC, hAC        *hcnode     // huffman roots for DC and AC coefficients
                                // use hDC for 1st sample, hAC for all others
    dUnits          [][64]int   // up to vSF rows of hSF data units (64 int)
    previousDC      int         // previous DC value for this component
    nUnitsRow       uint        // n units per row = nSamplesLines/8
    hSF, vSF        uint        // horizontal & vertical sampling factors
    dUCol           uint        // increments with each dUI till it reaches hSF
    dURow           uint        // increments with each row till it reaches vSF
    dUAnchor        uint        // top-left corner of dUnits area, incremented
                                // by hSF each time hSF*vSF data units are done
    count           uint8       // current sample count [0-63] in each data unit

    However, DC and AC samples are preprocessed and in particular AC samples
    are runlength compressed before entropy compression: a single preprocessed
    AC sample can represent many 0 samples - EOB indicates that all following
    samples are 0 until the end of block (64), ZRL indicates that the sixteen
    following samples are 0 and any non-zero sample can be preceded by up to
    15 zero samples.
*/
func (jpg *JpegDesc) getMcuDesc( sComp *[]scanCompRef ) *mcuDesc {

    mcu := new(mcuDesc)
    mcu.sComps = make( []scanComp, len(*sComp) )

    for i, sc := range( *sComp ) {
        cmp := jpg.components[sc.CMId]
        mcu.sComps[i].hDC = jpg.hdefs[2*sc.DCId].root   // AC follows DC
        mcu.sComps[i].hAC = jpg.hdefs[2*sc.ACId+1].root // (2 tables per dest)
        nUnitsRow := ((jpg.resolution.nSamplesLine / jpg.resolution.mhSF) *
                      cmp.hSF)/8
        nUnitsRstInt := jpg.nMcuRST * cmp.hSF
        if nUnitsRstInt > nUnitsRow {
            nUnitsRow = nUnitsRstInt
        }
        mcu.sComps[i].nUnitsRow = nUnitsRow
        mcu.sComps[i].hSF = cmp.hSF
        mcu.sComps[i].vSF = cmp.vSF
        // preallocate vSF * nUnitsLine for this component
        mcu.sComps[i].dUnits = make( [][64]int, cmp.vSF * nUnitsRow )
        // previousDC, dUCol, dURow, dUAnchor, nRows, count are set to 0
    }
    return mcu      // initially count is 0
}

func getMcuFormat( sc *scan ) string {

    nCmp := len( sc.mcuD.sComps )
    if nCmp != 3 && nCmp != 1 { panic("Unsupported MCU format\n") }

    var mcuf []byte = make( []byte, 32 )  // assume max res for all comp
    var cType1, cType2 byte

    j := 0
    for i, c := range( sc.mcuD.sComps ) {
        switch i {
        case 0:
            cType1, cType2 = 'Y', 0
        case 1:
            cType1, cType2 = 'C', 'b'
        case 2:
            cType2 = 'r'
        }
        for row := uint(0); row < c.vSF; row ++ {
            for col := uint(0); col < c.hSF; col++ {
                mcuf[j] = cType1
                if cType2 != 0 { mcuf[j+1] = cType2; j++ }
                mcuf[j+1] = byte(row + '0')
                mcuf[j+2] = byte(col + '0')
                j += 3
            }
        }
    }
    return string(mcuf[:j])
}

func (jpg *JpegDesc) startOfFrame( tag uint, sLen uint ) error {
    if jpg.Content {
        fmt.Printf( "SOF%d\n", tag & 0x0f )
    }
    if jpg.state != _FRAME {
        return fmt.Errorf( "startOfFrame: Wrong sequence %s in state %s\n",
                           getJPEGTagName(tag), jpg.getJPEGStateName() )
    }
    if sLen < 8 {
        return fmt.Errorf( "startOfFrame: Wrong SOF%d header (len %d)\n", tag & 0x0f, sLen )
    }

    offset := jpg.offset + 4
    nComponents := uint(jpg.data[offset+5])
    if sLen < 8 + (nComponents * 3) {
        return fmt.Errorf( "startOfFrame: Wrong SOF%d header (len %d for %d components)\n",
                           tag & 0x0f, sLen, nComponents )
    }
    samplePrecision := uint(jpg.data[offset])
    nLines := uint(jpg.data[offset+1]) << 8 + uint(jpg.data[offset+2])
    nSamples := uint(jpg.data[offset+3]) << 8 + uint(jpg.data[offset+4])
    offset += 6

    if jpg.Content {
        if (nSamples % 8) != 0 {
            fmt.Printf("  Warning: Samples/Line (%d) is not a multiple of 8\n", nSamples )
        }
        fmt.Printf( "  Lines: %d, Samples/Line: %d, sample precision: %d components: %d\n",
                    nLines, nSamples, samplePrecision, nComponents )
    }

    var maxHSF, maxVSF uint
    for i := uint(0); i < nComponents; i++ {
        cId := uint(jpg.data[offset])
        hSF := uint(jpg.data[offset+1])
        vSF := hSF & 0x0f
        hSF >>= 4
        QS := uint(jpg.data[offset+2])

        if hSF > maxHSF { maxHSF = hSF }
        if vSF > maxVSF { maxVSF = vSF }
        jpg.components = append( jpg.components, component{ cId, hSF, vSF, QS } )
        if jpg.Content {
            fmt.Printf( "    Component #%d Id %d Sampling factors H:V=%d:%d, Quantization selector %d\n",
                        i, cId, hSF, vSF, QS )
        }
        offset += 3
    }

    jpg.resolution.samplePrecision = samplePrecision
    jpg.resolution.nLines = nLines
    jpg.resolution.nSamplesLine = nSamples
    jpg.resolution.mhSF = maxHSF
    jpg.resolution.mvSF = maxVSF

    err := jpg.addTable( tag, jpg.offset, jpg.offset + 2 + sLen, original )
    jpg.scans = append( jpg.scans, scan{ } )    // ready for the first scan (yet unknown)
    jpg.state = _SCAN1
    if err != nil { return jpgForwardError( "startOfFrame", err ) }
    return nil
}

func (jpg *JpegDesc) processScanHeader( sLen uint ) error {

    offset := jpg.offset + 4
    nComponents := uint(jpg.data[offset])

    offset += 1
    if sLen != 6 + nComponents * 2 {
        return fmt.Errorf( "processScanHeader: Wrong SOS header (len %d for %d components)\n",
                           sLen, nComponents )
    }

    sCs := make( []scanCompRef, int(nComponents) )
    for i := uint(0); i < nComponents; i++ {
        sCs[i].CMId = uint(jpg.data[offset])
        eCTS := uint(jpg.data[offset+1])
        sCs[i].DCId = eCTS >> 4
        sCs[i].ACId = eCTS & 0x0f
        offset += 2
    }

    scan := jpg.getCurrentScan()
    if scan == nil { panic("Internal error (no frame for scan)\n") }
    scan.mcuD = jpg.getMcuDesc( &sCs )

    if jpg.Content {
        startSS := jpg.data[offset]
        endSS := jpg.data[offset+1]
        ssABP := jpg.data[offset+2]

        fmt.Printf( "  Components: %d\n", nComponents )
        for _, sC := range sCs {
            fmt.Printf( "    Selector 0x%x, DC entropy coding 0x%x, AC entropy coding 0x%x\n",
                        sC.CMId, sC.DCId, sC.ACId )
        }
        fmt.Printf( "  Spectral selection Start 0x%x, End 0x%x\n", startSS, endSS )
        fmt.Printf( "  Successive approximation bit position, high 0x%x low 0x%x\n", ssABP >> 4, ssABP & 0x0f )
        mcuFormat := getMcuFormat( scan )

        if nComponents == 3 {
            fmt.Printf( "  Interleaved YCbCr" )
        } else {
            fmt.Printf( "  Grayscale Y" )
        }
        fmt.Printf( ": MCUs format %s\n", mcuFormat )
    }
    return nil
}

var rlCodes [][]int = [][]int{
   { 0 },
   { -1,  1 },
   { -3, -2,  2,  3 },
   { -7, -6, -5, -4,  4,  5,  6,  7 },
   { -15, -14, -13, -12, -11, -10, -9, -8,
      8,  9,  10,  11,  12,  13,  14,  15 },
   { -31, -30, -29, -28, -27, -26, -25, -24,
     -23, -22, -21, -20, -19, -18, -17, -16,
      16,  17,  18,  19,  20,  21,  22,  23,
      24,  25,  26,  27,  28,  29,  30,  31 },
   { -63, -62, -61, -60, -59, -58, -57, -56,
     -55, -54, -53, -52, -51, -50, -49, -48,
     -47, -46, -45, -44, -43, -42, -41, -40,
     -39, -38, -37, -36, -35, -34, -33, -32,
      32,  33,  34,  35,  36,  37,  38,  39,
      40,  41,  42,  43,  44,  45,  46,  47,
      48,  49,  50,  51,  52,  53,  54,  55,
      56,  57,  58,  59,  60,  61,  62,  63 },
   { -127, -126, -125, -124, -123, -122, -121, -120,
     -119, -118, -117, -116, -115, -114, -113, -112,
     -111, -110, -109, -108, -107, -106, -105, -104,
     -103, -102, -101, -100, -99, -98, -97, -96,
     -95, -94, -93, -92, -91, -90, -89, -88,
     -87, -86, -85, -84, -83, -82, -81, -80,
     -79, -78, -77, -76, -75, -74, -73, -72,
     -71, -70, -69, -68, -67, -66, -65, -64,
      64,  65,  66,  67,  68,  69,  70,  71,
      72,  73,  74,  75,  76,  77,  78,  79,
      80,  81,  82,  83,  84,  85,  86,  87,
      88,  89,  90,  91,  92,  93,  94,  95,
      96,  97,  98,  99,  100,  101,  102,  103,
      104,  105,  106,  107,  108,  109,  110,  111,
      112,  113,  114,  115,  116,  117,  118,  119,
      120,  121,  122,  123,  124,  125,  126,  127 },
   { -255, -254, -253, -252, -251, -250, -249, -248,
     -247, -246, -245, -244, -243, -242, -241, -240,
     -239, -238, -237, -236, -235, -234, -233, -232,
     -231, -230, -229, -228, -227, -226, -225, -224,
     -223, -222, -221, -220, -219, -218, -217, -216,
     -215, -214, -213, -212, -211, -210, -209, -208,
     -207, -206, -205, -204, -203, -202, -201, -200,
     -199, -198, -197, -196, -195, -194, -193, -192,
     -191, -190, -189, -188, -187, -186, -185, -184,
     -183, -182, -181, -180, -179, -178, -177, -176,
     -175, -174, -173, -172, -171, -170, -169, -168,
     -167, -166, -165, -164, -163, -162, -161, -160,
     -159, -158, -157, -156, -155, -154, -153, -152,
     -151, -150, -149, -148, -147, -146, -145, -144,
     -143, -142, -141, -140, -139, -138, -137, -136,
     -135, -134, -133, -132, -131, -130, -129, -128,
      128,  129,  130,  131,  132,  133,  134,  135,
      136,  137,  138,  139,  140,  141,  142,  143,
      144,  145,  146,  147,  148,  149,  150,  151,
      152,  153,  154,  155,  156,  157,  158,  159,
      160,  161,  162,  163,  164,  165,  166,  167,
      168,  169,  170,  171,  172,  173,  174,  175,
      176,  177,  178,  179,  180,  181,  182,  183,
      184,  185,  186,  187,  188,  189,  190,  191,
      192,  193,  194,  195,  196,  197,  198,  199,
      200,  201,  202,  203,  204,  205,  206,  207,
      208,  209,  210,  211,  212,  213,  214,  215,
      216,  217,  218,  219,  220,  221,  222,  223,
      224,  225,  226,  227,  228,  229,  230,  231,
      232,  233,  234,  235,  236,  237,  238,  239,
      240,  241,  242,  243,  244,  245,  246,  247,
      248,  249,  250,  251,  252,  253,  254,  255 },
   { -511, -510, -509, -508, -507, -506, -505, -504,
     -503, -502, -501, -500, -499, -498, -497, -496,
     -495, -494, -493, -492, -491, -490, -489, -488,
     -487, -486, -485, -484, -483, -482, -481, -480,
     -479, -478, -477, -476, -475, -474, -473, -472,
     -471, -470, -469, -468, -467, -466, -465, -464,
     -463, -462, -461, -460, -459, -458, -457, -456,
     -455, -454, -453, -452, -451, -450, -449, -448,
     -447, -446, -445, -444, -443, -442, -441, -440,
     -439, -438, -437, -436, -435, -434, -433, -432,
     -431, -430, -429, -428, -427, -426, -425, -424,
     -423, -422, -421, -420, -419, -418, -417, -416,
     -415, -414, -413, -412, -411, -410, -409, -408,
     -407, -406, -405, -404, -403, -402, -401, -400,
     -399, -398, -397, -396, -395, -394, -393, -392,
     -391, -390, -389, -388, -387, -386, -385, -384,
     -383, -382, -381, -380, -379, -378, -377, -376,
     -375, -374, -373, -372, -371, -370, -369, -368,
     -367, -366, -365, -364, -363, -362, -361, -360,
     -359, -358, -357, -356, -355, -354, -353, -352,
     -351, -350, -349, -348, -347, -346, -345, -344,
     -343, -342, -341, -340, -339, -338, -337, -336,
     -335, -334, -333, -332, -331, -330, -329, -328,
     -327, -326, -325, -324, -323, -322, -321, -320,
     -319, -318, -317, -316, -315, -314, -313, -312,
     -311, -310, -309, -308, -307, -306, -305, -304,
     -303, -302, -301, -300, -299, -298, -297, -296,
     -295, -294, -293, -292, -291, -290, -289, -288,
     -287, -286, -285, -284, -283, -282, -281, -280,
     -279, -278, -277, -276, -275, -274, -273, -272,
     -271, -270, -269, -268, -267, -266, -265, -264,
     -263, -262, -261, -260, -259, -258, -257, -256,
      256,  257,  258,  259,  260,  261,  262,  263,
      264,  265,  266,  267,  268,  269,  270,  271,
      272,  273,  274,  275,  276,  277,  278,  279,
      280,  281,  282,  283,  284,  285,  286,  287,
      288,  289,  290,  291,  292,  293,  294,  295,
      296,  297,  298,  299,  300,  301,  302,  303,
      304,  305,  306,  307,  308,  309,  310,  311,
      312,  313,  314,  315,  316,  317,  318,  319,
      320,  321,  322,  323,  324,  325,  326,  327,
      328,  329,  330,  331,  332,  333,  334,  335,
      336,  337,  338,  339,  340,  341,  342,  343,
      344,  345,  346,  347,  348,  349,  350,  351,
      352,  353,  354,  355,  356,  357,  358,  359,
      360,  361,  362,  363,  364,  365,  366,  367,
      368,  369,  370,  371,  372,  373,  374,  375,
      376,  377,  378,  379,  380,  381,  382,  383,
      384,  385,  386,  387,  388,  389,  390,  391,
      392,  393,  394,  395,  396,  397,  398,  399,
      400,  401,  402,  403,  404,  405,  406,  407,
      408,  409,  410,  411,  412,  413,  414,  415,
      416,  417,  418,  419,  420,  421,  422,  423,
      424,  425,  426,  427,  428,  429,  430,  431,
      432,  433,  434,  435,  436,  437,  438,  439,
      440,  441,  442,  443,  444,  445,  446,  447,
      448,  449,  450,  451,  452,  453,  454,  455,
      456,  457,  458,  459,  460,  461,  462,  463,
      464,  465,  466,  467,  468,  469,  470,  471,
      472,  473,  474,  475,  476,  477,  478,  479,
      480,  481,  482,  483,  484,  485,  486,  487,
      488,  489,  490,  491,  492,  493,  494,  495,
      496,  497,  498,  499,  500,  501,  502,  503,
      504,  505,  506,  507,  508,  509,  510,  511 },
   { -1023, -1022, -1021, -1020, -1019, -1018, -1017, -1016,
     -1015, -1014, -1013, -1012, -1011, -1010, -1009, -1008,
     -1007, -1006, -1005, -1004, -1003, -1002, -1001, -1000,
     -999, -998, -997, -996, -995, -994, -993, -992,
     -991, -990, -989, -988, -987, -986, -985, -984,
     -983, -982, -981, -980, -979, -978, -977, -976,
     -975, -974, -973, -972, -971, -970, -969, -968,
     -967, -966, -965, -964, -963, -962, -961, -960,
     -959, -958, -957, -956, -955, -954, -953, -952,
     -951, -950, -949, -948, -947, -946, -945, -944,
     -943, -942, -941, -940, -939, -938, -937, -936,
     -935, -934, -933, -932, -931, -930, -929, -928,
     -927, -926, -925, -924, -923, -922, -921, -920,
     -919, -918, -917, -916, -915, -914, -913, -912,
     -911, -910, -909, -908, -907, -906, -905, -904,
     -903, -902, -901, -900, -899, -898, -897, -896,
     -895, -894, -893, -892, -891, -890, -889, -888,
     -887, -886, -885, -884, -883, -882, -881, -880,
     -879, -878, -877, -876, -875, -874, -873, -872,
     -871, -870, -869, -868, -867, -866, -865, -864,
     -863, -862, -861, -860, -859, -858, -857, -856,
     -855, -854, -853, -852, -851, -850, -849, -848,
     -847, -846, -845, -844, -843, -842, -841, -840,
     -839, -838, -837, -836, -835, -834, -833, -832,
     -831, -830, -829, -828, -827, -826, -825, -824,
     -823, -822, -821, -820, -819, -818, -817, -816,
     -815, -814, -813, -812, -811, -810, -809, -808,
     -807, -806, -805, -804, -803, -802, -801, -800,
     -799, -798, -797, -796, -795, -794, -793, -792,
     -791, -790, -789, -788, -787, -786, -785, -784,
     -783, -782, -781, -780, -779, -778, -777, -776,
     -775, -774, -773, -772, -771, -770, -769, -768,
     -767, -766, -765, -764, -763, -762, -761, -760,
     -759, -758, -757, -756, -755, -754, -753, -752,
     -751, -750, -749, -748, -747, -746, -745, -744,
     -743, -742, -741, -740, -739, -738, -737, -736,
     -735, -734, -733, -732, -731, -730, -729, -728,
     -727, -726, -725, -724, -723, -722, -721, -720,
     -719, -718, -717, -716, -715, -714, -713, -712,
     -711, -710, -709, -708, -707, -706, -705, -704,
     -703, -702, -701, -700, -699, -698, -697, -696,
     -695, -694, -693, -692, -691, -690, -689, -688,
     -687, -686, -685, -684, -683, -682, -681, -680,
     -679, -678, -677, -676, -675, -674, -673, -672,
     -671, -670, -669, -668, -667, -666, -665, -664,
     -663, -662, -661, -660, -659, -658, -657, -656,
     -655, -654, -653, -652, -651, -650, -649, -648,
     -647, -646, -645, -644, -643, -642, -641, -640,
     -639, -638, -637, -636, -635, -634, -633, -632,
     -631, -630, -629, -628, -627, -626, -625, -624,
     -623, -622, -621, -620, -619, -618, -617, -616,
     -615, -614, -613, -612, -611, -610, -609, -608,
     -607, -606, -605, -604, -603, -602, -601, -600,
     -599, -598, -597, -596, -595, -594, -593, -592,
     -591, -590, -589, -588, -587, -586, -585, -584,
     -583, -582, -581, -580, -579, -578, -577, -576,
     -575, -574, -573, -572, -571, -570, -569, -568,
     -567, -566, -565, -564, -563, -562, -561, -560,
     -559, -558, -557, -556, -555, -554, -553, -552,
     -551, -550, -549, -548, -547, -546, -545, -544,
     -543, -542, -541, -540, -539, -538, -537, -536,
     -535, -534, -533, -532, -531, -530, -529, -528,
     -527, -526, -525, -524, -523, -522, -521, -520,
     -519, -518, -517, -516, -515, -514, -513, -512,
      512,  513,  514,  515,  516,  517,  518,  519,
      520,  521,  522,  523,  524,  525,  526,  527,
      528,  529,  530,  531,  532,  533,  534,  535,
      536,  537,  538,  539,  540,  541,  542,  543,
      544,  545,  546,  547,  548,  549,  550,  551,
      552,  553,  554,  555,  556,  557,  558,  559,
      560,  561,  562,  563,  564,  565,  566,  567,
      568,  569,  570,  571,  572,  573,  574,  575,
      576,  577,  578,  579,  580,  581,  582,  583,
      584,  585,  586,  587,  588,  589,  590,  591,
      592,  593,  594,  595,  596,  597,  598,  599,
      600,  601,  602,  603,  604,  605,  606,  607,
      608,  609,  610,  611,  612,  613,  614,  615,
      616,  617,  618,  619,  620,  621,  622,  623,
      624,  625,  626,  627,  628,  629,  630,  631,
      632,  633,  634,  635,  636,  637,  638,  639,
      640,  641,  642,  643,  644,  645,  646,  647,
      648,  649,  650,  651,  652,  653,  654,  655,
      656,  657,  658,  659,  660,  661,  662,  663,
      664,  665,  666,  667,  668,  669,  670,  671,
      672,  673,  674,  675,  676,  677,  678,  679,
      680,  681,  682,  683,  684,  685,  686,  687,
      688,  689,  690,  691,  692,  693,  694,  695,
      696,  697,  698,  699,  700,  701,  702,  703,
      704,  705,  706,  707,  708,  709,  710,  711,
      712,  713,  714,  715,  716,  717,  718,  719,
      720,  721,  722,  723,  724,  725,  726,  727,
      728,  729,  730,  731,  732,  733,  734,  735,
      736,  737,  738,  739,  740,  741,  742,  743,
      744,  745,  746,  747,  748,  749,  750,  751,
      752,  753,  754,  755,  756,  757,  758,  759,
      760,  761,  762,  763,  764,  765,  766,  767,
      768,  769,  770,  771,  772,  773,  774,  775,
      776,  777,  778,  779,  780,  781,  782,  783,
      784,  785,  786,  787,  788,  789,  790,  791,
      792,  793,  794,  795,  796,  797,  798,  799,
      800,  801,  802,  803,  804,  805,  806,  807,
      808,  809,  810,  811,  812,  813,  814,  815,
      816,  817,  818,  819,  820,  821,  822,  823,
      824,  825,  826,  827,  828,  829,  830,  831,
      832,  833,  834,  835,  836,  837,  838,  839,
      840,  841,  842,  843,  844,  845,  846,  847,
      848,  849,  850,  851,  852,  853,  854,  855,
      856,  857,  858,  859,  860,  861,  862,  863,
      864,  865,  866,  867,  868,  869,  870,  871,
      872,  873,  874,  875,  876,  877,  878,  879,
      880,  881,  882,  883,  884,  885,  886,  887,
      888,  889,  890,  891,  892,  893,  894,  895,
      896,  897,  898,  899,  900,  901,  902,  903,
      904,  905,  906,  907,  908,  909,  910,  911,
      912,  913,  914,  915,  916,  917,  918,  919,
      920,  921,  922,  923,  924,  925,  926,  927,
      928,  929,  930,  931,  932,  933,  934,  935,
      936,  937,  938,  939,  940,  941,  942,  943,
      944,  945,  946,  947,  948,  949,  950,  951,
      952,  953,  954,  955,  956,  957,  958,  959,
      960,  961,  962,  963,  964,  965,  966,  967,
      968,  969,  970,  971,  972,  973,  974,  975,
      976,  977,  978,  979,  980,  981,  982,  983,
      984,  985,  986,  987,  988,  989,  990,  991,
      992,  993,  994,  995,  996,  997,  998,  999,
      1000,  1001,  1002,  1003,  1004,  1005,  1006,  1007,
      1008,  1009,  1010,  1011,  1012,  1013,  1014,  1015,
      1016,  1017,  1018,  1019,  1020,  1021,  1022,  1023 },
   { -2047, -2046, -2045, -2044, -2043, -2042, -2041, -2040,
     -2039, -2038, -2037, -2036, -2035, -2034, -2033, -2032,
     -2031, -2030, -2029, -2028, -2027, -2026, -2025, -2024,
     -2023, -2022, -2021, -2020, -2019, -2018, -2017, -2016,
     -2015, -2014, -2013, -2012, -2011, -2010, -2009, -2008,
     -2007, -2006, -2005, -2004, -2003, -2002, -2001, -2000,
     -1999, -1998, -1997, -1996, -1995, -1994, -1993, -1992,
     -1991, -1990, -1989, -1988, -1987, -1986, -1985, -1984,
     -1983, -1982, -1981, -1980, -1979, -1978, -1977, -1976,
     -1975, -1974, -1973, -1972, -1971, -1970, -1969, -1968,
     -1967, -1966, -1965, -1964, -1963, -1962, -1961, -1960,
     -1959, -1958, -1957, -1956, -1955, -1954, -1953, -1952,
     -1951, -1950, -1949, -1948, -1947, -1946, -1945, -1944,
     -1943, -1942, -1941, -1940, -1939, -1938, -1937, -1936,
     -1935, -1934, -1933, -1932, -1931, -1930, -1929, -1928,
     -1927, -1926, -1925, -1924, -1923, -1922, -1921, -1920,
     -1919, -1918, -1917, -1916, -1915, -1914, -1913, -1912,
     -1911, -1910, -1909, -1908, -1907, -1906, -1905, -1904,
     -1903, -1902, -1901, -1900, -1899, -1898, -1897, -1896,
     -1895, -1894, -1893, -1892, -1891, -1890, -1889, -1888,
     -1887, -1886, -1885, -1884, -1883, -1882, -1881, -1880,
     -1879, -1878, -1877, -1876, -1875, -1874, -1873, -1872,
     -1871, -1870, -1869, -1868, -1867, -1866, -1865, -1864,
     -1863, -1862, -1861, -1860, -1859, -1858, -1857, -1856,
     -1855, -1854, -1853, -1852, -1851, -1850, -1849, -1848,
     -1847, -1846, -1845, -1844, -1843, -1842, -1841, -1840,
     -1839, -1838, -1837, -1836, -1835, -1834, -1833, -1832,
     -1831, -1830, -1829, -1828, -1827, -1826, -1825, -1824,
     -1823, -1822, -1821, -1820, -1819, -1818, -1817, -1816,
     -1815, -1814, -1813, -1812, -1811, -1810, -1809, -1808,
     -1807, -1806, -1805, -1804, -1803, -1802, -1801, -1800,
     -1799, -1798, -1797, -1796, -1795, -1794, -1793, -1792,
     -1791, -1790, -1789, -1788, -1787, -1786, -1785, -1784,
     -1783, -1782, -1781, -1780, -1779, -1778, -1777, -1776,
     -1775, -1774, -1773, -1772, -1771, -1770, -1769, -1768,
     -1767, -1766, -1765, -1764, -1763, -1762, -1761, -1760,
     -1759, -1758, -1757, -1756, -1755, -1754, -1753, -1752,
     -1751, -1750, -1749, -1748, -1747, -1746, -1745, -1744,
     -1743, -1742, -1741, -1740, -1739, -1738, -1737, -1736,
     -1735, -1734, -1733, -1732, -1731, -1730, -1729, -1728,
     -1727, -1726, -1725, -1724, -1723, -1722, -1721, -1720,
     -1719, -1718, -1717, -1716, -1715, -1714, -1713, -1712,
     -1711, -1710, -1709, -1708, -1707, -1706, -1705, -1704,
     -1703, -1702, -1701, -1700, -1699, -1698, -1697, -1696,
     -1695, -1694, -1693, -1692, -1691, -1690, -1689, -1688,
     -1687, -1686, -1685, -1684, -1683, -1682, -1681, -1680,
     -1679, -1678, -1677, -1676, -1675, -1674, -1673, -1672,
     -1671, -1670, -1669, -1668, -1667, -1666, -1665, -1664,
     -1663, -1662, -1661, -1660, -1659, -1658, -1657, -1656,
     -1655, -1654, -1653, -1652, -1651, -1650, -1649, -1648,
     -1647, -1646, -1645, -1644, -1643, -1642, -1641, -1640,
     -1639, -1638, -1637, -1636, -1635, -1634, -1633, -1632,
     -1631, -1630, -1629, -1628, -1627, -1626, -1625, -1624,
     -1623, -1622, -1621, -1620, -1619, -1618, -1617, -1616,
     -1615, -1614, -1613, -1612, -1611, -1610, -1609, -1608,
     -1607, -1606, -1605, -1604, -1603, -1602, -1601, -1600,
     -1599, -1598, -1597, -1596, -1595, -1594, -1593, -1592,
     -1591, -1590, -1589, -1588, -1587, -1586, -1585, -1584,
     -1583, -1582, -1581, -1580, -1579, -1578, -1577, -1576,
     -1575, -1574, -1573, -1572, -1571, -1570, -1569, -1568,
     -1567, -1566, -1565, -1564, -1563, -1562, -1561, -1560,
     -1559, -1558, -1557, -1556, -1555, -1554, -1553, -1552,
     -1551, -1550, -1549, -1548, -1547, -1546, -1545, -1544,
     -1543, -1542, -1541, -1540, -1539, -1538, -1537, -1536,
     -1535, -1534, -1533, -1532, -1531, -1530, -1529, -1528,
     -1527, -1526, -1525, -1524, -1523, -1522, -1521, -1520,
     -1519, -1518, -1517, -1516, -1515, -1514, -1513, -1512,
     -1511, -1510, -1509, -1508, -1507, -1506, -1505, -1504,
     -1503, -1502, -1501, -1500, -1499, -1498, -1497, -1496,
     -1495, -1494, -1493, -1492, -1491, -1490, -1489, -1488,
     -1487, -1486, -1485, -1484, -1483, -1482, -1481, -1480,
     -1479, -1478, -1477, -1476, -1475, -1474, -1473, -1472,
     -1471, -1470, -1469, -1468, -1467, -1466, -1465, -1464,
     -1463, -1462, -1461, -1460, -1459, -1458, -1457, -1456,
     -1455, -1454, -1453, -1452, -1451, -1450, -1449, -1448,
     -1447, -1446, -1445, -1444, -1443, -1442, -1441, -1440,
     -1439, -1438, -1437, -1436, -1435, -1434, -1433, -1432,
     -1431, -1430, -1429, -1428, -1427, -1426, -1425, -1424,
     -1423, -1422, -1421, -1420, -1419, -1418, -1417, -1416,
     -1415, -1414, -1413, -1412, -1411, -1410, -1409, -1408,
     -1407, -1406, -1405, -1404, -1403, -1402, -1401, -1400,
     -1399, -1398, -1397, -1396, -1395, -1394, -1393, -1392,
     -1391, -1390, -1389, -1388, -1387, -1386, -1385, -1384,
     -1383, -1382, -1381, -1380, -1379, -1378, -1377, -1376,
     -1375, -1374, -1373, -1372, -1371, -1370, -1369, -1368,
     -1367, -1366, -1365, -1364, -1363, -1362, -1361, -1360,
     -1359, -1358, -1357, -1356, -1355, -1354, -1353, -1352,
     -1351, -1350, -1349, -1348, -1347, -1346, -1345, -1344,
     -1343, -1342, -1341, -1340, -1339, -1338, -1337, -1336,
     -1335, -1334, -1333, -1332, -1331, -1330, -1329, -1328,
     -1327, -1326, -1325, -1324, -1323, -1322, -1321, -1320,
     -1319, -1318, -1317, -1316, -1315, -1314, -1313, -1312,
     -1311, -1310, -1309, -1308, -1307, -1306, -1305, -1304,
     -1303, -1302, -1301, -1300, -1299, -1298, -1297, -1296,
     -1295, -1294, -1293, -1292, -1291, -1290, -1289, -1288,
     -1287, -1286, -1285, -1284, -1283, -1282, -1281, -1280,
     -1279, -1278, -1277, -1276, -1275, -1274, -1273, -1272,
     -1271, -1270, -1269, -1268, -1267, -1266, -1265, -1264,
     -1263, -1262, -1261, -1260, -1259, -1258, -1257, -1256,
     -1255, -1254, -1253, -1252, -1251, -1250, -1249, -1248,
     -1247, -1246, -1245, -1244, -1243, -1242, -1241, -1240,
     -1239, -1238, -1237, -1236, -1235, -1234, -1233, -1232,
     -1231, -1230, -1229, -1228, -1227, -1226, -1225, -1224,
     -1223, -1222, -1221, -1220, -1219, -1218, -1217, -1216,
     -1215, -1214, -1213, -1212, -1211, -1210, -1209, -1208,
     -1207, -1206, -1205, -1204, -1203, -1202, -1201, -1200,
     -1199, -1198, -1197, -1196, -1195, -1194, -1193, -1192,
     -1191, -1190, -1189, -1188, -1187, -1186, -1185, -1184,
     -1183, -1182, -1181, -1180, -1179, -1178, -1177, -1176,
     -1175, -1174, -1173, -1172, -1171, -1170, -1169, -1168,
     -1167, -1166, -1165, -1164, -1163, -1162, -1161, -1160,
     -1159, -1158, -1157, -1156, -1155, -1154, -1153, -1152,
     -1151, -1150, -1149, -1148, -1147, -1146, -1145, -1144,
     -1143, -1142, -1141, -1140, -1139, -1138, -1137, -1136,
     -1135, -1134, -1133, -1132, -1131, -1130, -1129, -1128,
     -1127, -1126, -1125, -1124, -1123, -1122, -1121, -1120,
     -1119, -1118, -1117, -1116, -1115, -1114, -1113, -1112,
     -1111, -1110, -1109, -1108, -1107, -1106, -1105, -1104,
     -1103, -1102, -1101, -1100, -1099, -1098, -1097, -1096,
     -1095, -1094, -1093, -1092, -1091, -1090, -1089, -1088,
     -1087, -1086, -1085, -1084, -1083, -1082, -1081, -1080,
     -1079, -1078, -1077, -1076, -1075, -1074, -1073, -1072,
     -1071, -1070, -1069, -1068, -1067, -1066, -1065, -1064,
     -1063, -1062, -1061, -1060, -1059, -1058, -1057, -1056,
     -1055, -1054, -1053, -1052, -1051, -1050, -1049, -1048,
     -1047, -1046, -1045, -1044, -1043, -1042, -1041, -1040,
     -1039, -1038, -1037, -1036, -1035, -1034, -1033, -1032,
     -1031, -1030, -1029, -1028, -1027, -1026, -1025, -1024,
      1024,  1025,  1026,  1027,  1028,  1029,  1030,  1031,
      1032,  1033,  1034,  1035,  1036,  1037,  1038,  1039,
      1040,  1041,  1042,  1043,  1044,  1045,  1046,  1047,
      1048,  1049,  1050,  1051,  1052,  1053,  1054,  1055,
      1056,  1057,  1058,  1059,  1060,  1061,  1062,  1063,
      1064,  1065,  1066,  1067,  1068,  1069,  1070,  1071,
      1072,  1073,  1074,  1075,  1076,  1077,  1078,  1079,
      1080,  1081,  1082,  1083,  1084,  1085,  1086,  1087,
      1088,  1089,  1090,  1091,  1092,  1093,  1094,  1095,
      1096,  1097,  1098,  1099,  1100,  1101,  1102,  1103,
      1104,  1105,  1106,  1107,  1108,  1109,  1110,  1111,
      1112,  1113,  1114,  1115,  1116,  1117,  1118,  1119,
      1120,  1121,  1122,  1123,  1124,  1125,  1126,  1127,
      1128,  1129,  1130,  1131,  1132,  1133,  1134,  1135,
      1136,  1137,  1138,  1139,  1140,  1141,  1142,  1143,
      1144,  1145,  1146,  1147,  1148,  1149,  1150,  1151,
      1152,  1153,  1154,  1155,  1156,  1157,  1158,  1159,
      1160,  1161,  1162,  1163,  1164,  1165,  1166,  1167,
      1168,  1169,  1170,  1171,  1172,  1173,  1174,  1175,
      1176,  1177,  1178,  1179,  1180,  1181,  1182,  1183,
      1184,  1185,  1186,  1187,  1188,  1189,  1190,  1191,
      1192,  1193,  1194,  1195,  1196,  1197,  1198,  1199,
      1200,  1201,  1202,  1203,  1204,  1205,  1206,  1207,
      1208,  1209,  1210,  1211,  1212,  1213,  1214,  1215,
      1216,  1217,  1218,  1219,  1220,  1221,  1222,  1223,
      1224,  1225,  1226,  1227,  1228,  1229,  1230,  1231,
      1232,  1233,  1234,  1235,  1236,  1237,  1238,  1239,
      1240,  1241,  1242,  1243,  1244,  1245,  1246,  1247,
      1248,  1249,  1250,  1251,  1252,  1253,  1254,  1255,
      1256,  1257,  1258,  1259,  1260,  1261,  1262,  1263,
      1264,  1265,  1266,  1267,  1268,  1269,  1270,  1271,
      1272,  1273,  1274,  1275,  1276,  1277,  1278,  1279,
      1280,  1281,  1282,  1283,  1284,  1285,  1286,  1287,
      1288,  1289,  1290,  1291,  1292,  1293,  1294,  1295,
      1296,  1297,  1298,  1299,  1300,  1301,  1302,  1303,
      1304,  1305,  1306,  1307,  1308,  1309,  1310,  1311,
      1312,  1313,  1314,  1315,  1316,  1317,  1318,  1319,
      1320,  1321,  1322,  1323,  1324,  1325,  1326,  1327,
      1328,  1329,  1330,  1331,  1332,  1333,  1334,  1335,
      1336,  1337,  1338,  1339,  1340,  1341,  1342,  1343,
      1344,  1345,  1346,  1347,  1348,  1349,  1350,  1351,
      1352,  1353,  1354,  1355,  1356,  1357,  1358,  1359,
      1360,  1361,  1362,  1363,  1364,  1365,  1366,  1367,
      1368,  1369,  1370,  1371,  1372,  1373,  1374,  1375,
      1376,  1377,  1378,  1379,  1380,  1381,  1382,  1383,
      1384,  1385,  1386,  1387,  1388,  1389,  1390,  1391,
      1392,  1393,  1394,  1395,  1396,  1397,  1398,  1399,
      1400,  1401,  1402,  1403,  1404,  1405,  1406,  1407,
      1408,  1409,  1410,  1411,  1412,  1413,  1414,  1415,
      1416,  1417,  1418,  1419,  1420,  1421,  1422,  1423,
      1424,  1425,  1426,  1427,  1428,  1429,  1430,  1431,
      1432,  1433,  1434,  1435,  1436,  1437,  1438,  1439,
      1440,  1441,  1442,  1443,  1444,  1445,  1446,  1447,
      1448,  1449,  1450,  1451,  1452,  1453,  1454,  1455,
      1456,  1457,  1458,  1459,  1460,  1461,  1462,  1463,
      1464,  1465,  1466,  1467,  1468,  1469,  1470,  1471,
      1472,  1473,  1474,  1475,  1476,  1477,  1478,  1479,
      1480,  1481,  1482,  1483,  1484,  1485,  1486,  1487,
      1488,  1489,  1490,  1491,  1492,  1493,  1494,  1495,
      1496,  1497,  1498,  1499,  1500,  1501,  1502,  1503,
      1504,  1505,  1506,  1507,  1508,  1509,  1510,  1511,
      1512,  1513,  1514,  1515,  1516,  1517,  1518,  1519,
      1520,  1521,  1522,  1523,  1524,  1525,  1526,  1527,
      1528,  1529,  1530,  1531,  1532,  1533,  1534,  1535,
      1536,  1537,  1538,  1539,  1540,  1541,  1542,  1543,
      1544,  1545,  1546,  1547,  1548,  1549,  1550,  1551,
      1552,  1553,  1554,  1555,  1556,  1557,  1558,  1559,
      1560,  1561,  1562,  1563,  1564,  1565,  1566,  1567,
      1568,  1569,  1570,  1571,  1572,  1573,  1574,  1575,
      1576,  1577,  1578,  1579,  1580,  1581,  1582,  1583,
      1584,  1585,  1586,  1587,  1588,  1589,  1590,  1591,
      1592,  1593,  1594,  1595,  1596,  1597,  1598,  1599,
      1600,  1601,  1602,  1603,  1604,  1605,  1606,  1607,
      1608,  1609,  1610,  1611,  1612,  1613,  1614,  1615,
      1616,  1617,  1618,  1619,  1620,  1621,  1622,  1623,
      1624,  1625,  1626,  1627,  1628,  1629,  1630,  1631,
      1632,  1633,  1634,  1635,  1636,  1637,  1638,  1639,
      1640,  1641,  1642,  1643,  1644,  1645,  1646,  1647,
      1648,  1649,  1650,  1651,  1652,  1653,  1654,  1655,
      1656,  1657,  1658,  1659,  1660,  1661,  1662,  1663,
      1664,  1665,  1666,  1667,  1668,  1669,  1670,  1671,
      1672,  1673,  1674,  1675,  1676,  1677,  1678,  1679,
      1680,  1681,  1682,  1683,  1684,  1685,  1686,  1687,
      1688,  1689,  1690,  1691,  1692,  1693,  1694,  1695,
      1696,  1697,  1698,  1699,  1700,  1701,  1702,  1703,
      1704,  1705,  1706,  1707,  1708,  1709,  1710,  1711,
      1712,  1713,  1714,  1715,  1716,  1717,  1718,  1719,
      1720,  1721,  1722,  1723,  1724,  1725,  1726,  1727,
      1728,  1729,  1730,  1731,  1732,  1733,  1734,  1735,
      1736,  1737,  1738,  1739,  1740,  1741,  1742,  1743,
      1744,  1745,  1746,  1747,  1748,  1749,  1750,  1751,
      1752,  1753,  1754,  1755,  1756,  1757,  1758,  1759,
      1760,  1761,  1762,  1763,  1764,  1765,  1766,  1767,
      1768,  1769,  1770,  1771,  1772,  1773,  1774,  1775,
      1776,  1777,  1778,  1779,  1780,  1781,  1782,  1783,
      1784,  1785,  1786,  1787,  1788,  1789,  1790,  1791,
      1792,  1793,  1794,  1795,  1796,  1797,  1798,  1799,
      1800,  1801,  1802,  1803,  1804,  1805,  1806,  1807,
      1808,  1809,  1810,  1811,  1812,  1813,  1814,  1815,
      1816,  1817,  1818,  1819,  1820,  1821,  1822,  1823,
      1824,  1825,  1826,  1827,  1828,  1829,  1830,  1831,
      1832,  1833,  1834,  1835,  1836,  1837,  1838,  1839,
      1840,  1841,  1842,  1843,  1844,  1845,  1846,  1847,
      1848,  1849,  1850,  1851,  1852,  1853,  1854,  1855,
      1856,  1857,  1858,  1859,  1860,  1861,  1862,  1863,
      1864,  1865,  1866,  1867,  1868,  1869,  1870,  1871,
      1872,  1873,  1874,  1875,  1876,  1877,  1878,  1879,
      1880,  1881,  1882,  1883,  1884,  1885,  1886,  1887,
      1888,  1889,  1890,  1891,  1892,  1893,  1894,  1895,
      1896,  1897,  1898,  1899,  1900,  1901,  1902,  1903,
      1904,  1905,  1906,  1907,  1908,  1909,  1910,  1911,
      1912,  1913,  1914,  1915,  1916,  1917,  1918,  1919,
      1920,  1921,  1922,  1923,  1924,  1925,  1926,  1927,
      1928,  1929,  1930,  1931,  1932,  1933,  1934,  1935,
      1936,  1937,  1938,  1939,  1940,  1941,  1942,  1943,
      1944,  1945,  1946,  1947,  1948,  1949,  1950,  1951,
      1952,  1953,  1954,  1955,  1956,  1957,  1958,  1959,
      1960,  1961,  1962,  1963,  1964,  1965,  1966,  1967,
      1968,  1969,  1970,  1971,  1972,  1973,  1974,  1975,
      1976,  1977,  1978,  1979,  1980,  1981,  1982,  1983,
      1984,  1985,  1986,  1987,  1988,  1989,  1990,  1991,
      1992,  1993,  1994,  1995,  1996,  1997,  1998,  1999,
      2000,  2001,  2002,  2003,  2004,  2005,  2006,  2007,
      2008,  2009,  2010,  2011,  2012,  2013,  2014,  2015,
      2016,  2017,  2018,  2019,  2020,  2021,  2022,  2023,
      2024,  2025,  2026,  2027,  2028,  2029,  2030,  2031,
      2032,  2033,  2034,  2035,  2036,  2037,  2038,  2039,
      2040,  2041,  2042,  2043,  2044,  2045,  2046,  2047 },
  }

func printDataUnit( dU *[64]int ) {
    for r := 0; r < 8; r++ {
        if r == 0 {
            fmt.Printf( "Data Unit:" )
        } else {
            fmt.Printf( "\n          " )
        }
        for c := 0; c < 8; c++ {
            fmt.Printf(" %04d", (*dU)[zigZagRowCol[r][c]] )
        }
    }
    fmt.Printf( "\n" )
}

func (jpg *JpegDesc) getBits( startByte, val uint, startBit, nBits uint8 ) string {

//    fmt.Printf("startByte %#x val %#x startBit=%d nBits=%d\n",
//                startByte, val, startBit, nBits)
    if startBit >= 8 { panic("startBit >= 8") }

    var buf bytes.Buffer
    if nBits == 0 {
        fmt.Fprintf( &buf, "offset=%#x [%#02x    .--------]",
                    startByte, jpg.data[startByte])
//        fmt.Fprintf( &buf, "                               ")
    } else {
//offset=0x269 [0xf5       00111---] Huffman: size 7 (0-runlength 0)
//offset=0x26a [0xf512     -----101 0001----] DC: decoded=81 cumulative=81
        fmt.Fprintf( &buf, "offset=%#x [%#02x",
                    startByte, jpg.data[startByte])

        xBytes :=  (startBit + nBits-1) / 8
        for i:= uint8(1); i <= xBytes; i++ {
            fmt.Fprintf( &buf, "%02x", jpg.data[startByte+uint(i)])
        }
        for i:= xBytes; i < 2; i++ {
        	buf.Write([]byte("  "))
        }
	    buf.Write([]byte("."))
        nBytes := xBytes + 1

        //e.g. -----101 0001----
        var i uint8
        for ; i < startBit; i++ {
            buf.Write([]byte("-"))
        }

        if val > 0xffff { panic("val > 0xffff") }

        s := int(startBit)      // might become negative during operations
        n := int(nBits)

        if s + n <= 8 {
            fmt.Fprintf( &buf, "%0*b", n, val )
        } else {
            fmt.Fprintf( &buf, "%0*b", 8 - s, val >> uint(s + n - 8) )
            buf.Write([]byte(" "))
            s -= 8
            if s + n > 8 { // at most 7 + 16
                //fmt.Printf( "reminings bits=%d\n", s + n
                v := val >> uint(s + n - 8)
                fmt.Fprintf( &buf, "%0*b", 8, v & 0xff )
                buf.Write([]byte(" "))
                s -= 8
            }
            //fmt.Printf( "reminings bits=%d\n", s + n
            val &= ((1 << uint(s + n)) -1)
            fmt.Fprintf( &buf, "%0*b", s + n, val )
        }
        i += nBits
        //fmt.Printf( "nBytes=%d\n", nBytes )
        //    fmt.Fprintf( &buf, "%0*b", nBits, val )

        for ; i < nBytes * 8; i++ {
            if i % 8 == 0   {
                buf.Write([]byte(" "))
            }
            buf.Write([]byte("-"))
        }
        buf.Write([]byte("]"))
    }
    return buf.String()
}

func (jpg *JpegDesc) processECS( nMCUs uint) (uint, error) {

    scan := jpg.getCurrentScan()
    if scan == nil { panic("Internal error (no scan for ECS)\n") }

    /*  after ach RST, reset previousDC, dUAnchor, dUCol, dURow & count
        for each scan component (Y[,Cb,Cr]) */
    for i := len(scan.mcuD.sComps)-1; i >= 0; i-- {
        scan.mcuD.sComps[i].previousDC = 0
        scan.mcuD.sComps[i].dUAnchor = 0  // RST could happen in the middle
        scan.mcuD.sComps[i].dUCol = 0     
        scan.mcuD.sComps[i].dURow = 0
        scan.mcuD.sComps[i].count = 0
    }
/*
    Each scan component (sComp) gives the number of dataUnits that the
    component can use (hSF *vSF). This is a small rectangular area whose
    top-left corner is located at dUAnchor in the dUnits array. Area units
    are located at:
        dUnits[dUAnchor+(nUnitsRow * dUcol) + dUrow], for dUcol in [0, vSF-1]
                                                      and dUrow in [0, hSF-1].
    Once the number of vSF * hSF data units have been processed for the same
    component, unitAnchor is incremented by hSF for the next area, dUrow and
    dUcol are reset to 0 for the next area, and sCompIndex is incremented
    modulo the number of components (len(mcusDsc.sComps)).

    Once UnitAnchor is found above nUnitsRow, the whole dUnits array is copied
    to the component DCT area for further processing, unitAnchor is reset to 0
    and the same dUnits array is reused for the next slice of data units
*/
    sCompIndex := 0                     // first component in MCU (Y)
    sComp := &scan.mcuD.sComps[0]       // first component definition
    dUnit := &sComp.dUnits[0]           // first data unit in component

/*
    Within a data unit, the first sample is always DC and the 63 following
    samples are AC samples. DC and AC samples are always encoded as a tuple
    of 2 symbols of varying length: (runSize, code)
    - runSize is huffman encoded with either DC or AC huffman table. In case of
      DC, runSize is just the size of the following code in bits. In case of AC
      it is split into 4-bit runlength of preceding zeros, and 4-bit size of
      the following code.
    - code is an offset in rlCodes[size], depending on the previous size:
      sample value = rlCodes[size][code]

    2 special runSize values are defined:
    - EOB = 0x00, indicates the end of non-zero samples. EOB applies only to
      AC samples. In this case all following samples, till the end of the data
      unit are set to 0 and no more samples for the data unit are expected.
    - ZRL = 0xf0, indicates a series of 16 zero samples. ZRL applies only to AC
      samples.
*/
    var curHcnode *hcnode = sComp.hDC   // always start with encoded DC
    huffman := true                     // if true runSize, else code

    var curByte, nBits uint8            // hold current encoded bits
    var runLen, size uint8              // current decoded runlength & size
    var codeBit uint8                   // n bits in current code
    var code uint                       // current code data

    // encoded loop 1 byte at a time: start at 1st byte following header or RST
    tLen := uint(len( jpg.data ))
    i := jpg.offset

    var huffbits uint8                  // number of bits encoding value (limited to 16)
    var huffval uint                    // decoded value

    // for pretty print formatting:
    var startByte = i                   // offset of the first byte contributing to code
    var startBit uint8                  // bit offset into startByte

encodedLoop:
    for ; i < tLen-1; i ++ {
        curByte = jpg.data[i]           // load next byte
        nBits = 8                       // 8 bits now available in curByte

        if curByte == 0xFF {
            i++         // skip expected following 0x00
            if i >= tLen-1 || jpg.data[i] != 0x00 {
                i--     // backup for next marker and stop
                if jpg.Mcu && jpg.Begin <= nMCUs && jpg.End >= nMCUs {
                    fmt.Printf( "MCU=%d comp=%d du=%d,%d offset=%#x [%#02x] End of scan segment (found marker or RST)\n",
                                nMCUs, sCompIndex, sComp.dURow, sComp.dUCol, i, curByte )
                }

                warning := false
                for k := len(scan.mcuD.sComps)-1; k >= 0; k-- {
                    if scan.mcuD.sComps[k].dUAnchor != 0 || scan.mcuD.sComps[k].dURow != 0 ||
                       scan.mcuD.sComps[k].dUCol != 0 || scan.mcuD.sComps[k].count != 0 {
                        warning = true
                        fmt.Printf( "Warning: incomplete component %d (%d rows): anchor %d (max %d) row %d col %d count %d\n",
                                k, scan.mcuD.sComps[k].nRows,
                                scan.mcuD.sComps[k].dUAnchor,
                                scan.mcuD.sComps[k].nUnitsRow,
                                scan.mcuD.sComps[k].dURow,
                                scan.mcuD.sComps[k].dUCol,
                                scan.mcuD.sComps[k].count )
                    }
                }
                if warning {
                    fmt.Printf("MCU=%d comp=%d du=%d,%d offset=%#x [%#02x] Unexpected end of scan segment\n",
                                nMCUs, sCompIndex, sComp.dURow, sComp.dUCol, i, curByte )
                }
                break                   // return condition
            }
        }
        for {                           // curbyte bit loop
            if huffman {
                for {                       // huffman bit loop (both DC & AC)
                    if nBits == 0 { continue encodedLoop } // need more bits
                        
                    if (curByte & 0x80) == 0x80 {
                        if curHcnode.left == nil { panic("Invalid code/huffman tree (left)\n") }
                        curHcnode = curHcnode.left
                        huffval <<= 1
                        huffval ++
                    } else {
                        if curHcnode.right == nil { panic("Invalid code/huffman tree (right)\n") }
                        curHcnode = curHcnode.right
                        huffval <<= 1
                    }

                    curByte <<= 1
                    nBits --
                    huffbits ++

                    if curHcnode.left == nil && curHcnode.right == nil {
                        runSize := curHcnode.symbol // if AC first 4 bits are
                        runLen = runSize >> 4      // runlength, remaining 4
                        size = runSize & 0x0f      // are size in all cases
                        if jpg.Mcu && jpg.Begin <= nMCUs && jpg.End >= nMCUs {
                            fmt.Printf( "MCU=%d comp=%d du=%d,%d %s Huffman: size %d (0-runlength %d)\n",
                                        nMCUs, sCompIndex, sComp.dURow, sComp.dUCol,
                                        jpg.getBits( startByte, uint(huffval), startBit, huffbits ),
                                        size, runLen )
                        }
                        startBit += huffbits
                        huffval = 0
                        huffbits = 0
                        huffman = false // next <size> bits are the code
                        codeBit = 0     // 0 bit of following code is extracted
                        code = 0
                        break           // end huffman bit loop
                    }
                }
            } else {                        // extract size bits of code
                if ( sComp.count == 0 ) {   // first code is for DC
                    if size > 11 {      // code bits to extract from curByte
                        return nMCUs, fmt.Errorf("processECS: DC coef size (%d) > 11 bits\n", size)
                    }

                    for ; codeBit < size; codeBit++ {   // extract code bits
                        if nBits == 0 { continue encodedLoop }  // need more bits

//                    fmt.Printf( "[MCU=%d nBits=%d] DC: codeBit=%d size=%d code=%#02x\n",
//                                nMCUs, 8-nBits, codeBit, size, code )
                        code <<= 1
                        if curByte & 0x80 == 0x80 {
                            code += 1
                        }
                        curByte <<= 1
                        nBits --
                    }
                    decodedDC := rlCodes[size][code]
                    sComp.previousDC += decodedDC

                    if jpg.Mcu && jpg.Begin <= nMCUs && jpg.End >= nMCUs {
                        fmt.Printf( "MCU=%d comp=%d du=%d,%d %s DC: decoded=%d cumulative=%d\n",
                                    nMCUs, sCompIndex, sComp.dURow, sComp.dUCol,
                                    jpg.getBits( startByte, code, startBit, size ),
                                    decodedDC, sComp.previousDC )
//  fmt.Printf( "[MCU=%d offset=%#x.%d component=%d] DC: size=0x%x, code=%#b (%#02x), decodedDC=%d cumulative DC=%d\n",
//              nMCUs, i, 8-nBits, sCompIndex, size, code, code, decodedDC, sComp.previousDC )
                    }

                    (*dUnit)[0] = sComp.previousDC  // store in first slot of current data unit

                    startBit += size
                    sComp.count = 1         // 1 sample (DC) processed
                    curHcnode = sComp.hAC   // ready for following AC symbols
                    huffman = true

                }  else {                   // AC values
                    if runLen == 0 && size == 0 { // EOB => following AC coefs are 0
                        if jpg.Mcu && jpg.Begin <= nMCUs && jpg.End >= nMCUs {
                            fmt.Printf( "MCU=%d comp=%d du=%d,%d %s AC: EOB for this data unit\n",
                                    nMCUs, sCompIndex, sComp.dURow, sComp.dUCol,
                                    jpg.getBits( startByte, code, startBit, size ) )
                        }
                        for n:= sComp.count; n < 64; n++ {  // in zigzag order
                            (*dUnit)[n]  = 0
                        }
                        sComp.count = 64     // ready for next data unit

                    } else if runLen == 15 && size == 0 {   // ZRL => 16 0s
                        if jpg.Mcu && jpg.Begin <= nMCUs && jpg.End >= nMCUs {
                            fmt.Printf( "MCU=%d comp=%d du=%d,%d %s AC: ZRL => next 16 values are 0\n",
                                    nMCUs, sCompIndex, sComp.dURow, sComp.dUCol,
                                    jpg.getBits( startByte, code, startBit, size ) )
                        }
                        if sComp.count+16 > 64 {
                            return nMCUs, fmt.Errorf("processECS: ZRL over the end of data uint\n")
                        }
                        for n:= uint8(0); n < 16; n++ {     // in zigzag order
                            (*dUnit)[sComp.count+n] = 0
                        }
                        sComp.count += 16

                    } else {                // not a special case, size is not 0
                        if size < 1 || size > 10 {
                            return nMCUs, fmt.Errorf("processECS: AC coef size (%d) not in [1-10] bits\n",
                                                      size)
                        }
                        for ; codeBit < size; codeBit++ {
                            if nBits == 0 { continue encodedLoop }  // need more bits

//                        fmt.Printf( "[MCU=%d offset=%#x.%d] AC: codeBit=%d, size=%d, code=%#02x\n",
//                                    nMCUs, i, 8-nBits, codeBit, size, code )
                            code <<= 1
                            if curByte & 0x80 == 0x80 {
                                code += 1
                            }
                            curByte <<= 1
                            nBits --

                        }
                        decodedAC := rlCodes[size][code]

                        if jpg.Mcu && jpg.Begin <= nMCUs && jpg.End >= nMCUs {
                        fmt.Printf( "MCU=%d comp=%d du=%d,%d %s AC: runlength %d decoded=%d\n",
                                    nMCUs, sCompIndex, sComp.dURow, sComp.dUCol,
                                    jpg.getBits( startByte, code, startBit, size ),
                                    runLen, decodedAC )
                        }
                        if sComp.count+runLen > 64 {
                            return nMCUs, fmt.Errorf("processECS: Runlength %d over the end of data uint\n",
                                                     runLen)
                        }
                        for n:= uint8(0); n < runLen; n++ { // in zigzag order
                            (*dUnit)[sComp.count+n] = 0
                        }
                        sComp.count += runLen
                        startBit += size
                        // store decoded AC in next slot of current data unit
                        (*dUnit)[sComp.count] = decodedAC
                        sComp.count++
                    }
                    if sComp.count == 64 {  // end of data unit
//                        printDataUnit( dUnit )
                        sComp.dUCol++
                        if sComp.dUCol >= sComp.hSF {
                            sComp.dUCol = 0
                            sComp.dURow++
                            if sComp.dURow >= sComp.vSF {
                                sComp.dURow = 0     // end of current component
                                sComp.dUAnchor += sComp.hSF // ready for next du
                                sCompIndex++
                                if sCompIndex >= len(scan.mcuD.sComps) {
                                    sCompIndex = 0
                                    nMCUs ++        // new MCU
                                }
//                                fmt.Printf("!!! Switching to component %d\n", sCompIndex)
                                sComp = &scan.mcuD.sComps[sCompIndex]
                                if sComp.dUAnchor == sComp.nUnitsRow { // end of DU slice
                                    if nMCUs % jpg.nMcuRST != 0 {
                                        fmt.Printf("Warning: end of slice @MCU %d is not synced with RST intervals (%d)\n",
                                                    nMCUs, jpg.nMcuRST )
                                    }
                                    for sci := 0; sci < len(scan.mcuD.sComps); sci++ {

                                        sc := &scan.mcuD.sComps[sci]
                                        for i := uint(0); i < sc.vSF; i ++ {
                                            sc.iDCTdata = append( sc.iDCTdata, iDCTRow{} )
                                            dctRow := len(sc.iDCTdata) - 1
                                            sc.iDCTdata[dctRow] = append( sc.iDCTdata[dctRow], sc.dUnits[
                                               (i*sc.nUnitsRow/sc.vSF) :
                                               (i*sc.nUnitsRow/sc.vSF)+(sc.nUnitsRow/sc.vSF)]... )
                                            sc.nRows++
                                        }
                                        sc.dUAnchor = 0
                                        sc.dURow = 0
                                        sc.dUCol = 0
                                        sc.count = 0
                                    }
                                }
                            }
                        }
                        sComp.count = 0
                        dUnit = &sComp.dUnits[sComp.dUAnchor +
                                              (sComp.nUnitsRow * sComp.dUCol) +
                                              sComp.dURow]
//                        fmt.Printf("Ready for next data unit: component %d anchor %d row %d col %d\n",
//                                    sCompIndex, sComp.dUAnchor, sComp.dURow, sComp.dUCol)
                        curHcnode = sComp.hDC   // new data unit starts with DC coefficient
                    } else {
                        curHcnode = sComp.hAC   // same data unit, keep working on AC
                    }
                    huffman = true
                    //panic ("Debug\n" )
                }
            }
//            fmt.Printf("startBit %d nBits %d\n", startBit, nBits)
            startBit %= 8
            if startBit == 0 {
                startByte = i+1
            } else {
                startByte = i
            }
        }
    }

    jpg.offset = i  // stopped at 0xFF followed by non-zero byte or at tLen-1
    return nMCUs, nil
}

func (jpg *JpegDesc) processScan( tag, sLen uint ) error {
    if jpg.Content {
        fmt.Printf( "SOS\n" )
    }
    if (jpg.state != _SCAN1 && jpg.state != _SCANn) {
        return fmt.Errorf( "processScan: Wrong sequence %s in state %s\n",
                            getJPEGTagName(tag), jpg.getJPEGStateName() )
    }
    if sLen < 6 {   // fixed size besides components
        return fmt.Errorf( "processScan: Wrong SOS header (len %d)\n", sLen )
    }

    if err := jpg.processScanHeader( sLen ); err != nil { return err }

    err := jpg.addTable( tag, jpg.offset, jpg.offset + 2 + sLen, original )
    if err != nil {
        return jpgForwardError( "processScan", err )
    }
    if jpg.state == _SCAN1 { jpg.state = _SCAN1_ECS } else { jpg.state = _SCANn_ECS }

    jpg.offset += sLen + 2
    firstECS := jpg.offset

    if jpg.Content {  fmt.Printf( "  %s @offset 0x%x\n",
                                  jpg.getJPEGStateName(), jpg.offset ) }
    rstCount := 0
    var lastRSTIndex, nIx uint
    var lastRST uint = 7
    tLen := uint(len( jpg.data ))   // start hunting for 0xFFxx with xx != 0x00

    var nMCus uint
    for ; ; {   // processECS return upon error, reached EOF or 0xFF followed by non-zero
        if nMCus, err = jpg.processECS( nMCus ); err != nil {
            return jpgForwardError( "processScan", err )
        }
        nIx = jpg.offset
        if nIx+1 >= tLen || jpg.data[nIx+1] < 0xd0 || jpg.data[nIx+1] > 0xd7 {
            break
        }       // else one of RST0-7 embedded in scan data, keep going

        RST := uint( jpg.data[nIx+1] - 0xd0 )
        if (lastRST + 1) % 8 != RST {
            if jpg.Fix {
                RST = (lastRST + 1) % 8
                jpg.data[nIx+1] = byte(0xd0 + RST)
                fmt.Printf( "  FIXING: setting RST sequence: %d\n", RST )
            } else {
                fmt.Printf( "  WARNING: invalid RST sequence (%d, expected %d)\n", RST, (lastRST + 1) % 8 )
            }
        }
        lastRSTIndex = nIx
        lastRST = RST
        rstCount++

        jpg.offset += 2;    // skip RST
    }
//    fmt.Printf( "End of scan @0x%08x (lastRst 0x%08x)\n", nIx, lastRSTIndex )
    if jpg.Content {
        fmt.Printf( "  Actual number of Mcus in scan %d\n", nMCus )
        fmt.Printf( "  %d restart intervals\n", rstCount )
    }

    if lastRSTIndex == nIx - 2 {
        if jpg.Fix {
            nIx -= 2
            fmt.Printf( "  FIXING: Removing ending RST (useless)\n" )
        } else {
            fmt.Printf( "  WARNING: ending RST is useless\n" )
        }
    }

    err = jpg.addECS( firstECS, nIx, original, nMCus )
    if err != nil {
        return jpgForwardError( "processScan", err )
    }
    jpg.scans = append( jpg.scans, scan{ } )    // ready for next scan
    jpg.state = _SCANn
    return nil
}

func (jpg *JpegDesc)defineRestartInterval( tag, sLen uint ) error {
    offset := jpg.offset + 4
    restartInterval := uint(jpg.data[offset]) << 8 + uint(jpg.data[offset+1])
    jpg.nMcuRST = restartInterval

    if jpg.Content {
        fmt.Printf( "DRI\n" )
        fmt.Printf( "  Restart Interval %d\n", restartInterval )
        if jpg.resolution.nSamplesLine % restartInterval != 0 {
            fmt.Printf( "  Warning number of samples per line (%d) is not a multiple of the restart interval\n",
                        jpg.resolution.nSamplesLine )
        }
    }
    return jpg.addTable( tag, jpg.offset, jpg.offset + 2 + sLen, original )
}

var zigZagRowCol = [8][8]int{{  0,  1,  5,  6, 14, 15, 27, 28 },
                             {  2,  4,  7, 13, 16, 26, 29, 42 },
                             {  3,  8, 12, 17, 25, 30, 41, 43 },
                             {  9, 11, 18, 24, 31, 40, 44, 53 },
                             { 10, 19, 23, 32, 39, 45, 52, 54 },
                             { 20, 22, 33, 38, 46, 51, 55, 60 },
                             { 21, 34, 37, 47, 50, 56, 59, 61 },
                             { 35, 36, 48, 49, 57, 58, 62, 63 }}

func (jpg *JpegDesc)printQuantizationMatrix( pq, tq uint ) {

    fmt.Printf( "  Zig-Zag: " )
    var f string
    if pq != 0 { f = "%5d " } else { f = "%3d " }

    for i := 0; ;  {
        for j := 0; j < 8; j++ {
            fmt.Printf( f, jpg.qdefs[tq].values[i+j] )
        }
        i += 8
        if i == 64 { break }
        fmt.Printf( "\n           " )
    }
    fmt.Printf( "\n" )

    for i := 0; i < 8; i++ {
        fmt.Printf( "  Row %d: [ ", i )
        for j := 0; j < 8; j++ {
            fmt.Printf( f, jpg.qdefs[tq].values[zigZagRowCol[i][j]] )
        }
        fmt.Printf("]\n")
    }
}

func (jpg *JpegDesc)defineQuantizationTable( tag, sLen uint ) ( err error ) {

    end := jpg.offset + 2 + sLen
    offset := jpg.offset + 4

    if jpg.Content { fmt.Printf( "DQT\n") }

    for qt := 0; ; qt++ {
        pq := uint(jpg.data[offset]) >> 4
        tq := uint(jpg.data[offset]) & 0x0f
        if pq > 1 {
            return fmt.Errorf( "defineQuantizationTable: Wrong precision (%d)\n", pq )
        }
        if tq > 3 {
            return fmt.Errorf( "defineQuantizationTable: Wrong destination (%d)\n", pq )
        }

        if jpg.Content {
            fmt.Printf( "  Quantization value precision %d destination %d\n",
                        8 * (pq+1), tq )
        }

        offset ++
        jpg.qdefs[tq].precision = pq != 0
        for i := 0; i < 64; i++ {
            jpg.qdefs[tq].values[i] = uint16(jpg.data[offset])
            offset ++
            if pq != 0 {
                jpg.qdefs[tq].values[i] <<= 8
                jpg.qdefs[tq].values[i] += uint16(jpg.data[offset])
                offset++
            }
        }
        if jpg.Quantizers {
            if ! jpg.Content {
                fmt.Printf( "Quantization table for destination %d\n", tq )
            }
            jpg.printQuantizationMatrix( pq, tq )
        }
        if offset >= end {
            break
        }
    }
    if offset != end {
        return fmt.Errorf( "defineQuantizationTable: Invalid DQT length: %d actual: %d\n",
                           sLen, offset - jpg.offset -2 )
    }
    return jpg.addTable( tag, jpg.offset, end, original )
}

func buildTree( huffDef *hdef ) {

    huffDef.root = new( hcnode )
    var last *hcnode = huffDef.root
    var level uint

    for i := uint(0); i < 16; i++ {
        cl := i + 1                                     // code length
        for _, symbol := range huffDef.cdefs[i].values {
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
                    last = last.parent
                    level--
                }
            }

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
}

func printTree( root *hcnode, indent string ) {
    fmt.Printf( "Huffman codes:\n" );

    var buffer  []uint8

    var printNodes func( n *hcnode )
    printNodes = func( n *hcnode ) {
        if n.left == nil && n.right == nil {
            fmt.Printf( "%s%s: 0x%02x\n", indent, string(buffer), n.symbol )
            buffer = buffer[:len(buffer)-1]
        } else {    // right is always present
            buffer = append( buffer, '0' )
            printNodes( n.right )
            if n.left != nil {
                buffer = append( buffer, '1' )
                printNodes( n.left )
            }
            n = n.parent;
            if n != nil {
                buffer = buffer[:len(buffer)-1]
            }
        }
    }
    printNodes( root )
}

func (jpg *JpegDesc)printHuffmanTable( td uint ) {

    fmt.Printf( "code lengths and symbols:\n" )

    var nSymbols uint
    for i := 0; i < 16; i++ {
        if jpg.hdefs[td].cdefs[i].length == 0 { continue }

        nSymbols += jpg.hdefs[td].cdefs[i].length
        fmt.Printf( "    length %2d: %3d symbols: [ ",
                    i+1, jpg.hdefs[td].cdefs[i].length )
VALUE_LOOP:
        for j := uint(0); ;  {
            for k := uint(0); k < 8; k++ {
                if j+k >= jpg.hdefs[td].cdefs[i].length { break VALUE_LOOP }
                fmt.Printf( "0x%02x ", jpg.hdefs[td].cdefs[i].values[j+k] )
            }
            fmt.Printf("\n                              ")
            j += 8
        }
        fmt.Printf( "]\n" )
    }
    fmt.Printf("    Total number of symbols: %d\n", nSymbols )
}

func (jpg *JpegDesc)defineHuffmanTable( tag, sLen uint ) ( err error ) {

    end := jpg.offset + 2 + sLen
    offset := jpg.offset + 4

    if jpg.Content { fmt.Printf( "DHT\n") }

    for ht := 0; ; ht++ {
        tc := uint(jpg.data[offset]) >> 4
        th := uint(jpg.data[offset]) & 0x0f

        if tc > 1 || th > 1 {
            return fmt.Errorf( "defineHuffmanTable: Wrong table class/destination (%d/%d)\n", tc, th )
        }

        var class string
        if tc == 0 { class = "DC" } else { class = "AC" }

        if jpg.Content {
            fmt.Printf( "  Huffman table class %s destination %d ", class, th )
        }

        td := 2*th+tc // use 2 tables, 1 for DC and 1 for AC, per destination
        offset++
        voffset := offset+16

        for hcli := uint(0); hcli < 16; hcli++ {
            jpg.hdefs[td].cdefs[hcli].length = uint(jpg.data[offset+hcli])
            for hvi := uint(0); hvi < jpg.hdefs[td].cdefs[hcli].length; hvi++ {
                jpg.hdefs[td].cdefs[hcli].values =
                   append( jpg.hdefs[td].cdefs[hcli].values, jpg.data[voffset+hvi] )
            }
            voffset += jpg.hdefs[td].cdefs[hcli].length
        }
        buildTree( &jpg.hdefs[td] )

        if jpg.Lengths {
            if ! jpg.Content {
                fmt.Printf( "Huffman table class %s destination %d ", class, th )
            }
            jpg.printHuffmanTable( td )
        }

        if jpg.Codes {
            var indent string = "    "
            if jpg.Lengths {
                fmt.Printf( "   " )
            } else if ! jpg.Content {
                fmt.Printf( "Huffman table class %s destination %d ", class, th )
                indent = "  "
            } 
            printTree( jpg.hdefs[td].root, indent )
        } else if jpg.Content && ! jpg.Lengths {
            fmt.Printf( "\n" )
        }

        offset = voffset;
        if offset >= end {
            break
        }
    }
    if offset != end {
        return fmt.Errorf( "defineHuffmanTable: Invalid DHT length: %d actual: %d\n", sLen, offset - jpg.offset -2 )
    }
    return jpg.addTable( tag, jpg.offset, end, original )
}

func (jpg *JpegDesc)commentSegment( tag, sLen uint ) error {
    if jpg.Content {
        offset := jpg.offset
        var b bytes.Buffer
        s := jpg.data[offset:offset+sLen]
        b.Write( s )
        fmt.Printf( "COM\n" )
        fmt.Printf( "  %s\n", b.String() )
    }
    return nil
}

func (jpg *JpegDesc)defineNumberOfLines( tag, sLen uint ) ( err error ) {
    if jpg.Content {
        fmt.Printf( "DNL\n" )
    }
    if jpg.state != _SCANn {
        return fmt.Errorf( "defineNumberOfLines: Wrong sequence %s in state %s\n",
                       getJPEGTagName(tag), jpg.getJPEGStateName() )
    }
    if sLen != 4 {   // fixed size
        return fmt.Errorf( "defineNumberOfLines: Wrong DNL header (len %d)\n", sLen )
    }
    if jpg.pDNL {
        return fmt.Errorf( "defineNumberOfLines: Multiple DNL tables\n" )
    }

    jpg.pDNL = true
    offset := jpg.offset + 4

    var nLines uint
    if jpg.Content {
        nLines = uint(jpg.data[offset]) << 8 + uint(jpg.data[offset+1])
        fmt.Printf( "  Number of Lines: %d\n", nLines )
    }
    jpg.gDNLnLines = nLines

    // find SOFn segment, which is the last table in global tables
    sof := jpg.getLastGlobalTable()
    if sof == nil || sof.stop - sof.start < 8 { panic("Internal error\n") }

    var prevLines uint
    if sof.from == original {
        prevLines = uint(jpg.data[sof.start + 5]) << 8 + uint(jpg.data[sof.start + 6])
    } else {
        prevLines = uint(jpg.update[sof.start + 5]) << 8 + uint(jpg.update[sof.start + 6])
    }

    if jpg.Fix {    // fix SOFn segment && remove DNL table
        if sof.from == original {
            jpg.data[sof.start + 5] = jpg.data[offset]
            jpg.data[sof.start + 6] = jpg.data[offset+1]
        } else {
            jpg.update[sof.start + 5] = jpg.data[offset]
            jpg.update[sof.start + 6] = jpg.data[offset+1]
        }
        fmt.Printf( "  FIXING: replacing number of lines in Start Of Frame (from %d to %d) and removing DNL table\n",
                    prevLines, nLines)
    } else {
        if ( prevLines != 0 ) {
            return fmt.Errorf( "defineNumberOfLines: DNL table found with non 0 SOF number of lines (%d)\n",
                                prevLines )
        }

        // otherwise just add the table
        err = jpg.addTable( tag, jpg.offset, jpg.offset + 2 + sLen, original )
    }
    return
}

func (jpg *JpegDesc)fixLines( ) {

    // find SOFn segment, which is the last table in global tables
    sof := jpg.getLastGlobalTable()
    if sof == nil || sof.stop - sof.start < 8 { panic("Internal error (no Start Of Frame)\n") }

    var prevLines uint
    if sof.from == original {
        prevLines = uint(jpg.data[sof.start + 5]) << 8 + uint(jpg.data[sof.start + 6])
    } else {
        prevLines = uint(jpg.update[sof.start + 5]) << 8 + uint(jpg.update[sof.start + 6])
    }

    n := len( jpg.scans ) -1    // last scan is empty
    if n == 0 { panic("Internal error (no scan for image)\n") }

    nLines := uint(0)   // calculate the actual number of lines from scan results
    for i:= 0; i < n; i++ {
        scan := &jpg.scans[i]
        if nLines < scan.mcuD.sComps[0].nRows {
            nLines = scan.mcuD.sComps[0].nRows
        }
    }
    nLines *= 8         // 8 pixel lines per data unit

    // fix image resolution (metadata)
    jpg.resolution.nLines = nLines

    // fix SOFn segment
    if sof.from == original {
        jpg.data[sof.start + 5] = byte(nLines >> 8)
        jpg.data[sof.start + 6] = byte(nLines&0xff)
    } else {
        jpg.update[sof.start + 5] = byte(nLines >> 8)
        jpg.update[sof.start + 6] = byte(nLines&0xff)
    }
    fmt.Printf( "  FIXING: replacing number of lines in Start Of Frame with actual scan results (from %d to %d)\n",
                prevLines, nLines)
}

func (jpg *JpegDesc)printMarker( tag, sLen, offset uint ) {
    if jpg.Markers {
        fmt.Printf( "Tag 0x%x, len %d, offset 0x%x (%s)\n", tag, sLen, offset, getJPEGTagName(tag) )
    }
}

type Control struct {
    Markers         bool    // show JPEG markers as they are parsed
    Content         bool    // display content of JPEG segments
    Quantizers      bool    // display quantization matrices as defined
    Lengths         bool    // display huffman table (code length & symbols)
    Codes           bool    // display huffman code for each symbol
    Mcu             bool    // display MCUs as they are parsed
    Du              bool    // display each DU resulting from MCU parsing
    Fix             bool    // try and fix errors if possible
    Begin, End      uint    // control MCU &DU display (from begin to end, included)
}

/*
    Analyse analyses jpeg data and splits the data into well-known segments.
    The argument toDo indicates what information should be printed during
    analysis. The argument doDo.Fix, if true, indicates that some common issues
    in jpeg data be fixed as much as possible during analysis and updated in
    memory.

    What can be fixed:

    - if the last RSTn is ending a scan it is not necessary and it may cause a
    renderer to fail. It is removed from the scan.

    - if a DNL table is found after an ECS and if the number of lines given in
    the SOFn table was 0, the number of lines found in DNL is set in the SOFn
    and in metadata and the DNL table is removed

    - if the number of lines calculated from the scan data is different from
    the SOFn value, the SOFn value and metadata are updated (this is done after
    DNL processing).

    It returns a tuple: a pointer to a JpegDesc containing segment definitions
    and an error. In all cases, nil error or not, the returned JpegDesc is
    usable (but wont be complete in case of error).
*/
func Analyze( data []byte, toDo *Control ) ( *JpegDesc, error ) {

    jpg := new( JpegDesc )   // initially in INIT state (0)
    jpg.Control = *toDo
    jpg.data = data

    if ! bytes.Equal( data[0:2],  []byte{ 0xff, 0xd8 } ) {
		return jpg, fmt.Errorf( "Analyse: Wrong signature 0x%x for a JPEG file\n", data[0:2] )
	}

    tLen := uint(len(data))
    for i := uint(0); i < tLen; {
        tag := uint(data[i]) << 8 + uint(data[i+1])
        sLen := uint(0)       // case of a segment without any data

        if tag < _TEM {
		    return jpg, fmt.Errorf( "Analyse: invalid marker 0x%x\n", data[i:i+1] )
        }

        switch tag {

        case _SOI:            // no data, no length
            jpg.printMarker( tag, sLen, i )
            if jpg.state != _INIT {
		        return jpg, fmt.Errorf( "Analyse: Wrong sequence %s in state %s\n",
                                        getJPEGTagName(tag), jpg.getJPEGStateName() )
            }
            jpg.state = _APPLICATION

        case _RST0, _RST1, _RST2, _RST3, _RST4, _RST5, _RST6, _RST7: // empty segment, no following length
            jpg.printMarker( tag, sLen, i )
            return jpg, fmt.Errorf ("Analyse: Marker %s hould not happen in top level segments\n",
                                     getJPEGTagName(tag) )

        case _EOI:
            jpg.printMarker( tag, sLen, i )
            if jpg.state != _SCAN1 && jpg.state != _SCANn {
		        return jpg, fmt.Errorf( "Analyse: Wrong sequence %s in state %s\n",
                            getJPEGTagName(tag), jpg.getJPEGStateName() )
            }
            jpg.state = _FINAL
            jpg.offset = i + 2  // points after the last byte
            if jpg.Fix { jpg.fixLines( ) }
            break

        default:        // all other cases have data following tag & length
            sLen = uint(data[i+2]) << 8 + uint(data[i+3])
            jpg.printMarker( tag, sLen, i )
            var err error

            switch tag {    // second level tag switching within the first default
            case _APP0:
                err = jpg.app0( tag, sLen )

            case _APP1, _APP2, _APP3, _APP4, _APP5, _APP6, _APP7, _APP8, _APP9,
                 _APP10, _APP11, _APP12, _APP13, _APP14, _APP15:

            case _SOF0, _SOF1, _SOF2, _SOF3, _SOF5, _SOF6, _SOF7, _SOF9, _SOF10,
                 _SOF11, _SOF13, _SOF14, _SOF15:
                err = jpg.startOfFrame( tag, sLen )

            case _DHT:  // Define Huffman Table
                err = jpg.defineHuffmanTable( tag, sLen )

            case _DQT:  // Define Quantization Table
                err = jpg.defineQuantizationTable( tag, sLen )

            case _DAC:    // Define Arithmetic coding
                err = jpg.addTable( tag, jpg.offset, jpg.offset + 2 + sLen, original )

            case _DNL:
                err = jpg.defineNumberOfLines( tag, sLen )

            case  _DRI:  // Define Restart Interval
                err = jpg.defineRestartInterval( tag, sLen )

            case _SOS:
                err = jpg.processScan( tag, sLen )
                if err != nil { return jpg, jpgForwardError( "Analyse", err ) }
                i = jpg.offset          // jpg.offset has been updated
                continue

            case _COM:  // Comment
                err = jpg.commentSegment( tag, sLen )

            case _DHP, _EXP:  // Define Hierarchical Progression, Expand reference components
                return jpg, fmt.Errorf( "Analyse: Unsupported hierarchical table %s\n",
                                        getJPEGTagName(tag) )

            default:    // All JPEG extensions and reserved tags (_JPG, _TEM, _RESn)
                return jpg, fmt.Errorf( "Analyse: Unsupported JPEG extension or reserved tag%s\n",
                                        getJPEGTagName(tag) )
            }
            if err != nil { return jpg, jpgForwardError( "Analyse", err ) }
            if jpg.state == _APPLICATION {
                jpg.state = _FRAME
            }
        }
        i += sLen + 2
        jpg.offset = i          // always points at the mark
    }
    return jpg, nil
}

/*
    ReadJpeg reads a JPEG file in memory, and starts analysing its content.
    The argument path is the existing file path.
    The argument toDo provides information about how to analyse the document
    If toDo.Fix is true, ReadJped fixes some common issues in jpeg data by
    writing the modified data in memory, so that they can be stored later by
    calling Write or Generate.

    It returns a tuple: a pointer to a JpegDesc containing the segment
    definitions and an error. If the file cannot be read the returned JpegDesc
    is nil.
*/
func ReadJpeg( path string, toDo *Control ) ( *JpegDesc, error ) {
    data, err := ioutil.ReadFile( path )
    if err != nil {
		return nil, fmt.Errorf( "ReadJpeg: Unable to read file %s: %v\n", path, err )
	}
    return Analyze( data, toDo )
}

// IsComplete returns true if the current JPEG data makes a complete JPEG file.
// It does not guarantee that the data corresponds to a valid JPEG image
func (jpg *JpegDesc) IsComplete( ) bool {
    return jpg.state == _FINAL
}

type Metadata struct {
    SampleSize      uint    // number of bits per pixel
    Width, Height   uint    // image size in pixels
}

// GetMetadata returns the sample size (precision) and image size (width, height).
func (jpg *JpegDesc)GetMetadata( ) Metadata {
    var result Metadata
    result.SampleSize = jpg.resolution.samplePrecision
    result.Width = jpg.resolution.nSamplesLine
    result.Height = jpg.resolution.nLines
    return result
}

// GetActualLengths returns the number of bytes between SOI and EOI (both included)
// in the possibly fixed jpeg data, and the original data length. The data length may be
// different if the analysis stopped in error, issues have been fixed or if there is some
// garbage at the end that should be ignored.
func (jpg *JpegDesc) GetActualLengths( ) ( uint, uint ) {

    dataSize := uint( len( jpg.data ) )
    if ! jpg.IsComplete() { return 0, dataSize }
    size, err := jpg.flatten( ioutil.Discard ); if err != nil { return 0, dataSize }
    return uint(size), dataSize
}

func (jpg *JpegDesc)writeSegment( w io.Writer, s *segment ) (written int, err error) {
    if s.from == original {
        written, err = w.Write( jpg.data[s.start:s.stop] )
    } else {
        written, err = w.Write( jpg.update[s.start:s.stop] )
    }
    return
}

func (jpg *JpegDesc)flatten( w io.Writer ) (int, error) {

    if ! jpg.IsComplete() {
        return 0, fmt.Errorf( "flatten: data is not a complete JPEG\n" )
    }
    written, err := w.Write( []byte{ 0xFF, 0xD8 } )
    if err != nil { return written, jpgForwardError( "flatten", err ) }

    var n int
    for index := range( jpg.tables )  {
        n, err = jpg.writeSegment( w, &jpg.tables[index] )
        written += n
        if err != nil { return  written,jpgForwardError( "flatten", err ) }
    }

    for scanIndex := range( jpg.scans ) {
        for tableIndex := range( jpg.scans[scanIndex].tables ) {
            n, err = jpg.writeSegment( w, &jpg.scans[scanIndex].tables[tableIndex] )
            written += n
            if err != nil { return written, jpgForwardError( "flatten", err ) }
        }
        for ECSIndex := range( jpg.scans[scanIndex].ECSs ) {
            n, err = jpg.writeSegment( w, &jpg.scans[scanIndex].ECSs[ECSIndex] )
            written += n
            if err != nil { return written, jpgForwardError( "flatten", err ) }
        }
    }

    n, err = w.Write( []byte{ 0xFF, 0xD9 } )
    written += n
    if err != nil { return written, jpgForwardError( "flatten", err ) }
    return written, nil
}

// Generate returns a copy in memory of the possibly fixed jpeg file after analysis.
func (jpg *JpegDesc) Generate( ) ( []byte, error ) {
    var b bytes.Buffer
    _, err := jpg.flatten( &b )
    if  err != nil { return nil, jpgForwardError( "Generate", err ) }
    return b.Bytes(), nil
}

// Write stores the possibly fixed JEPG data into a file.
// The argument path is the new file path.
// If the file exists already, new content will replace the existing one.
func (jpg *JpegDesc)Write( path string ) error {
    if ! jpg.IsComplete() {
        return fmt.Errorf( "Write: Data is not a complete JPEG\n" )
    }

	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.ModePerm)
    if err != nil { return jpgForwardError( "Write", err ) }

    _, err = jpg.flatten( f )
    if err != nil { return jpgForwardError( "Write", err ) }

    if err = f.Close( ); err != nil { return jpgForwardError( "Write", err ) }
    return nil
}

