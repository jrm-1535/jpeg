// Package jpeg provides a few primitives to parse and analyse a JPEG image
package jpeg

import (
    "fmt"
)

/*  ISO/IEC 10918-1:1993 defines JPEG document structure:

A JPEG document must start with 0xffd8 (Start Of Image) and end with 0xffd9
(End Of Image):
    0xffd8 <JPEG data> 0xffd9

Two cases of JPEG data are documented: non-hierarchical and hierarchical modes.

Non-hierarchical mode:
   SOI <optional tables> SOFn <optional tables><frame> EOI
        where Start Of Image (SOI) is 0xffd8, End Of Image (EOI) is 0xffd9

        Optional tables may appear immediately after SOI or immediately after
        frame header SOFn. They are:
            Application data (APP0 to APP15): ususally APP0 for JFIF, APP1 for
            exif, none required in a minimum JPEG file.
            Quantization Table (DQT), at least 1 required
            Huffman Table (DHT) required if the frame has a SOF1, SOF2, SOF3,
            SOF5, SOD6 or SOF7 header.
            Arithmetic Coding Table (DAC) required if the frame has a SOF9,
            SOF10, SOF11, SOF13, SOD14 or SOF15 header, and default table is
            not used,
            Define Restart Interval (DRI) required if RSTn markers are used.
            Comment, rarely used

        The frame data can be made of multiple scans:
            At least one scan segment,
            optionally followed by a number of lines segment (DNL): 0xffdc
            optionally followed by multiple other scan segments, each without
            a DNL segment:

            SOFn <scan segment #1>[DNL][<scan segment #2>...<last scan segment>

            A Frame header is a start of frame (SOFn): 0xffCn, where n is from
            0 to 15, minus multiples of 4 (SOF4, SOF8 and SOF12 do not exist).
            Each SOFn implies the following encoded scan data format, according
            to the n in SOFn

            All SOFn segments share the same syntax:
                SOfn,
                2 byte size,
                1 byte sample precision,
                2 byte number of lines,
                2 byte number of samples/line,
                1 byte number of following components, for each of those
                components:
                    1 byte unique component id,
                    4 bit  horizontal sampling factor (number of component H
                           units in each MCU)
                    4 bit  vertical sampling factor (number of component V
                           units in each MCU)
                    2 byte quantization table selector

            A DNL segment is 0xffdc 0x0002 0xnnnn where nnnn is the number of
            lines in the scan. It is optional and used only if the number of
            lines in the immediately preceding SOF is undefined (set to 0).

            A scan segment may start with optional tables followed by one
            mandatory scan header (SOS), followed by one entropy-coded segment
            (ECS), followed by multiple sequences of one RSTn (Restart) and one
            ECS (only if restart was enabled by a previous DRI table):
                [<optional tables>] SOS ECS [DNL] [RST0 ECS]...[RSTn ECS]

            RSTn indicates one restart interval from RST0 to RST7, starting
            from 0 and incrementing before wrapping around.

            A scan header segment is start of scan (SOS) segment: 0xffda with
            the following synrax
                SOS, 2 byte size, 1 byte number of components in scan,
                followed for each of those components:
                    1 byte component selector (in theory must match one of the
                           unique component ids in frame header, in practice
                           ignored since the order is fixed and identical to
                           the order of components in the fram header - YCbCr).
                    4 bit  DC entropy coding table selector
                    4 bit  AC entropy coding table selector
                    1 byte start of spectral or predictor selectopn
                    1 byte end of spectral or predictor selectopn
                    4 bit successive approximation bit position high
                    4 bit successive approximation bit position low

            An ECS segment is made of multiple <MCUs> each ECS with the same RI
            (Restart Interval) MCUs, except possibly the last one

            A Quantization Table (DQT) starts with 0xffdb, followed by 2 byte
            segment length, followed by a number n of tables:
            n = ( segment - len ) /  ( 65 + 64 * precisiom ), each table is:
                4  bit quantization element precision (0 =>8 bits, 1 => 16 bits)
                4  bit destination ID (0-3) referred to by start of frame
                       quantization table selector
                1 or 2 byte quantization table element (according to the
                        sample precision) * 64 elements

Hierarchical mode:
    SOI <optional tables><DHT><frame>...<frame> EOI
        Optional tables may appear immediately after SOI
        Hierarchical Progression Table (DHT) is mandatory before the first
        frame.
        The first frame is non-differential. The following ones may be
        non-differential or differential. Non-differential frames start with
        one of the following Start Of Frame markers: SOF0, SOF1, SOF2, SOF3,
        SOF9, SOF10 or SOF 11. Differential frames start with one of the
        following Start Of Frame markers: SOF5, SOF6, SOF7, SOF13, SOF14 or
        SOF15. Differencial frames must include an EXP segment if they require
        expansion horizontally or vertically.

    This case is not supported here.

*/

const (                         // JPEG parsing state
    _INIT = iota                // expecting SOI
    _APPLICATION                // from _INIT after SOI, expecting APPn (and APPn ext)
    _FRAME                      // from _APP after any table other than APP0
    _SCAN1                      // from _FRAME after SOFn, expecting DHT, DAC, DQT, DRI, COM, or SOS
    _SCAN1_ECS                  // from _SCAN1 after SOS, expecting ECSn/RStn, DHT, DAC, DQT, DRI, COM, SOS, DNL or EOI
    _SCANn                      // from _SCAN1_ECS, after DNL, expecting DHT, DAC, DQT, DRI, COM, SOS or EOI
    _SCANn_ECS                  // from _SCANn, after SOS, expecting ECSn/RStn, DHT, DAC, DQT, DRI, COM, SOS or EOI
    _FINAL                      // from either _SCAN1_ECS or _SCANn_ECS, after EOI
)

/* State transitions
 _INIT        -> _APPLICATION   transition on SOI
 _APPLICATION -> _FRAME         transition on any table other than APPn
 _FRAME       -> _SCAN1         transition on SOFn
 _SCAN1       -> _SCAN1_ECS     transition on SOS
 _SCAN1_ECS   -> _FINAL         transition on EOI
 _SCAN1_ECS   -> _SCANn         transition on DNL
 _SCANn       -> _SCANn_ECS     transition on SOS
 _SCANn_ECS   -> _FINAL         transition on EOI
*/

var stateNames = [...]string {
    "initial", "application", "frame",
    "first scan", "first scan encoded segment",
    "other scan", "other scan encoded segment",
    "final" }

func (jpg *JpegDesc) getJPEGStateName( ) string {
    if jpg.state > _FINAL {
        return "Unknown state"
    }
    return stateNames[ jpg.state ]
}

type source uint                // whether segment is from raw data or has been
                                // modified after fixing
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

type control struct {           // just to keep JpegDesc opaque
                    Control
}

// JpegDesc is the internal structure describing the JPEG file
type JpegDesc struct {
    data            []byte      // raw data file
    update          []byte      // modified data (only if fix is true and issues
                                // are encountered)
    offset          uint        // current offset in raw data file
    state           int         // INIT, APP, FRAME, SCAN1, SCAN1_ECS, SCANn,
                                // SCANn_ECS, FINAL
    app0Extension   bool        // APP0 followed by APP0 extension
    pDNL            bool        // DNL table processed
    gDNLnLines      uint        // DNL given nLines in picture
    nMcuRST         uint        // number of MCUs expected between RSTn

    qdefs           [4]qdef     // Quantization zig-zag coefficients for 4 dest
    hdefs           [8]hdef     // Huffman code definition for 4 dest * (DC+AC)

    apps            []app       // APP0(s), APP1, ...
    tables          []segment   // frame optional tables and 1 terminating SOFn
    components      []component // from SOFn component definitions
                                // note: component order is Y [, Cb, Cr] in SOFn
    resolution      sampling    // luminance (greyscale) or YCbCr picture
                                // sampling resolution
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

var markerNnames = [...]string {
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
    "APP15 Application Vendor Specific #15",

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

func getJPEGmarkerName( marker uint ) string {
    if marker == _TEM { return "TEM Temporary use in arithmetic coding" }
    if marker < _SOF0 || marker > _COM { return "RES Reserved Marker" }

    return markerNnames[ marker - _SOF0 ]
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

func (jpg *JpegDesc)addTable( marker, start, stop uint, from source ) error {
    table := segment{ from: from, start: start, stop: stop }
    if jpg.state == _APPLICATION || jpg.state == _FRAME {
        jpg.tables = append( jpg.tables, table )
    } else if jpg.state == _SCAN1 || jpg.state == _SCANn {
        scan := jpg.getCurrentScan()
        scan.tables = append( scan.tables, table )
    } else {
        return fmt.Errorf( "addTable: Wrong sequence %s in state %s\n",
                           getJPEGmarkerName(marker), jpg.getJPEGStateName() )
    }
    return nil
}

