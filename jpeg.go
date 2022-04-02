// Package jpeg provides a few primitives to parse and analyse a JPEG image
package jpeg

import (
    "fmt"
    "io"
    "io/ioutil"
    "bytes"
    "os"
    "bufio"
//    "time"
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
    _APPLICATION                // from _INIT after SOI, expecting APPn
    _FRAME                      // from _APP after any table other than APP
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

func (jpg *Desc) getJPEGStateName( ) string {
    if jpg.state > _FINAL {
        return "Unknown state"
    }
    return stateNames[ jpg.state ]
}

type dataUnit       [64]int16
type iDCTRow        []dataUnit  // dequantizised iDCT matrices (yet to inverse)

type scanComp struct {
    hDC, hAC        *hcnode     // huffman roots for DC and AC coefficients
                                // use hDC for 1st sample, hAC for all others
    dUnits          []dataUnit  // up to vSF rows of hSF data units (64 int)
    iDCTdata        []iDCTRow   // rows of reordered idata unit before iDCT
    previousDC      int16       // previous DC value for this component
    nUnitsRow       uint        // n units per row = nSamplesLines/8
    hSF, vSF        uint        // horizontal & vertical sampling factors
    dUCol           uint        // increments with each dUI till it reaches hSF
    dURow           uint        // increments with each row till it reaches vSF
    dUAnchor        uint        // top-left corner of dUnits area, incremented
                                // by hSF each time hSF*vSF data units are done
    nRows           uint        // number of rows already processed
    count           uint8       // current sample count [0-63] in each data unit
    cId             uint8       // component id from _SOS
    dcId, acId      uint8       // entropy table ids for DC & AC coefficients
    quId, quSz      uint8       // quantization table id and size
}

type mcuDesc struct {           // Minimum Coded Unit Descriptor
    sComps           []scanComp // one per scan component in order: Y, [Cb, Cr]
}

type scan   struct {            // one for each scan
    ECSs            []byte      // entropy coded segments constituting the scan
    mcuD            *mcuDesc    // MCU definition for the scan
    nMcus           uint        // total number of MCUs in scan
    rstInterval     uint        // nMCUs between restart intervals
    rstCount        uint        // total number of restart in the scan
    startSS, endSS  uint8       // start, end spectral selection
    sABPh, sABPl    uint8       // sucessive approximation bit position high, low
}

type hcnode struct {
    left, right     *hcnode
    parent          *hcnode
    symbol          uint8
}

type qdef struct {
    size            uint        // number of bits per value (8 or 16)
    values          [64]uint16  // actually often uint8, but may be uint16
}

type hdef struct {
    values          [16][]uint8
    root            *hcnode
}

type Component struct {
    Id, HSF, VSF, QS uint8
}

type Encoding  uint
const (
    HuffmanBaselineSequential Encoding = iota   // 1 frame
    HuffmanExtendedSequential                   // precision 8 or 12 bits
    HuffmanProgressive
    HuffmanLossless
    _                                       // skip DHT (4)
    DifferentialHuffmanSequential           // Differential == Hierarchical (n frames)
    DifferentialHuffmanProgressive
    DifferentialHuffmanLossless
    _                                       // skip JPEG extension (8)
    ArithmeticExtendedSequential
    ArithmeticProgressive
    ArithmeticLossless
    _                                       // skip  DAT (12)
    DifferentialArithmeticSequential
    DifferentialArithmeticProgressive
    DifferentialArithmeticLossless
)

func encodingString( c Encoding ) string {
    switch c {
    case HuffmanBaselineSequential:         return "Huffman Baseline Sequential DCT"
    case HuffmanExtendedSequential:         return "Huffman Extended Sequential DCT"
    case HuffmanProgressive:                return "Huffman ProgressiveDCT"
    case HuffmanLossless:                   return "Huffman Lossless"

    case DifferentialHuffmanSequential:     return "Differential Huffman Sequential DCT"
    case DifferentialHuffmanProgressive:    return "Differential Huffman Progressive DCT"
    case DifferentialHuffmanLossless:       return "Differential Huffman Lossless"

    case ArithmeticExtendedSequential:      return "Arithmetic Extended Sequential DCT"
    case ArithmeticProgressive:             return "Arithmetic Progressive DCT"
    case ArithmeticLossless:                return "Arithmetic Lossless"

    case DifferentialArithmeticSequential:  return "Differential Arithmetic Sequential DCT"
    case DifferentialArithmeticProgressive: return "Differential Arithmetic Progressive DCT"
    case DifferentialArithmeticLossless:    return "Differential Arithmetic Lossless"
    }
    return "Invalid encoding"
}

type EntropyCoding uint
const (
    HuffmanCoding EntropyCoding = iota
    ArithmeticCoding
)

func entropyCodingString( e EntropyCoding ) string {
    switch e {
    case HuffmanCoding:     return "Huffman Coding"
    case ArithmeticCoding:  return "Arithmetic Coding"
    }
    return "Unknown Entropy Coding"
}

type EncodingMode uint
const (
    BaselineSequential EncodingMode = iota // precision 8b 2+2 tables (DC+AC)
    ExtendedSequential                     // precision 8/12b, 4+4 tables
    ExtendedProgressive                    // multiple scans
    Lossless                               // precision [2..16]b 4 DC tables
)

func encodingModeString( m EncodingMode ) string {
    switch m {
    case BaselineSequential:    return "Baseline Sequential"
    case ExtendedSequential:    return "Extended Sequential"
    case ExtendedProgressive:   return "Extended Progressive"
    case Lossless:              return "Lossless"
    }
    return "Unknown Encoding Mode"
}

type Framing uint
const (
    SingleFrame Framing = iota          // non hierarchical modes
    HierarchicalFrames                  // DHP with Differential frames and not
)

func framing( c Encoding ) Framing {
    return Framing(( c % 8 ) / 4)
}

type sampling  struct {
    nLines          uint16      // number of lines from frame
    nSamplesLine    uint16      // number of samples per line
    dnlLines        uint16      // number of lines from DNL
    scanLines       uint16      // number of lines from scan
    samplePrecision uint8       // number of bits per sample
    mhSF, mvSF      uint8       // max horizontal and vertical sampling factors
}

type frame struct {             // one for each SOFn
    id              uint        // frame number [0..n] in appearance order
    encoding        Encoding    // how the frame is encoded
    resolution      sampling
    components      []Component // from SOFn component definitions
                                // note: component order is Y [, Cb, Cr] in SOFn
    scans           []scan      // for the scans following SOFn
    image           *Desc       // access to global image parameters
}

type VisualSide int
const (
    Left VisualSide = iota      // left side of the image
    Top                         // top side of the image
    Right                       // right side of the image
    Bottom                      // bottom side of the image
)

type VisualEffect int
const (
    None VisualEffect = iota    // no rotation, no mirror
    VerticalMirror              // mirror left => right
    Rotate90                    // +90 degrees   (right rotation)
    VerticalMirrorRotate90      // Vertical mirror + right rotation
    HorizontalMirror            // mirror top => bottom (upside down)
    Rotate180                   // +180 degrees (Vertical & Horizontal mirror)
    HorizontalMirrorRotate90    // Horizontal mirror + right rotation
    Rotate270                   // +270 degrees (left rotation)
)

type Orientation struct {
    AppSource       int         // id [0:15] of app segment providing info
                                // 0 if no orientation is available
    Row0            VisualSide  // how the first row must be aligned
    Col0            VisualSide  // how the first col must be aligned
    Effect          VisualEffect
}

type control struct {           // just to keep Desc opaque
                    Control
}

type segmenter interface {      // segment interface
    serialize( io.Writer ) (int, error)
    format( io.Writer ) (int, error)
}

// cumulative formatted writer
type cumulativeWriter struct {
    w       io.Writer
    count   int
    err     error
}
func newCumulativeWriter( w io.Writer ) *cumulativeWriter {
    cw := new( cumulativeWriter )
    cw.w = w
    return cw
}
func (cw *cumulativeWriter)format( f string, a ...interface{} ) {
    if cw.err != nil {
        return
    }
    n, err := fmt.Fprintf( cw.w, f, a... )
    cw.err = err
    cw.count += n
}
func (cw *cumulativeWriter)setError( err error ) {
    cw.err = err
}
func (cw *cumulativeWriter)Write( v []byte ) (n int, err error) {
    // implements Writer interface for use with fmt.Fprintf( cw, ... )
    if cw.err != nil {
        return 0, cw.err
    }
    n, err = cw.w.Write( v )
    cw.err = err
    cw.count += n
    return
}
func (cw *cumulativeWriter)result( ) (int, error) {
    return cw.count, cw.err
}

// Desc is the internal structure describing the JPEG file
type Desc struct {
    data            []byte      // raw data file
    update          []byte      // modified data (only if fix is true and issues
                                // are encountered)
    offset          uint        // current offset in raw data file
    state           int         // INIT, APP, FRAME, SCAN1, SCAN1_ECS, SCANn,
                                // SCANn_ECS, FINAL
    app0Extension   bool        // APP0 followed by APP0 extension
    nMcuRST         uint        // number of MCUs expected between RSTn
    orientation    *Orientation // nil if unknown in metadata

// global data applying to frames as they occur
    segments        []segmenter // segments in order they have occured

    process         Framing     // whether DHP or SOF
    qdefs           [4]qdef     // Quantization zig-zag coefficients for 4 dest
    hdefs           [8]hdef     // Huffman code definition for 4 dest * (DC+AC)

// frame slice with encoding, resolution and components & other private tables.
    frames          []frame

                    control     // what to print/fix during parsing
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
    _SOF7  = 0xffc7     // Start Of Frame Differential Huffman-coding frames (Lossless)
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
    _DRI   = 0xffdd     // Define Restart Interval
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
    "DRI Define Restart Interval",
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

var zigZagRowCol = [8][8]int{{  0,  1,  5,  6, 14, 15, 27, 28 },
                             {  2,  4,  7, 13, 16, 26, 29, 42 },
                             {  3,  8, 12, 17, 25, 30, 41, 43 },
                             {  9, 11, 18, 24, 31, 40, 44, 53 },
                             { 10, 19, 23, 32, 39, 45, 52, 54 },
                             { 20, 22, 33, 38, 46, 51, 55, 60 },
                             { 21, 34, 37, 47, 50, 56, 59, 61 },
                             { 35, 36, 48, 49, 57, 58, 62, 63 }}

func jpgForwardError( prefix string, err error ) error {
    return fmt.Errorf( prefix + ": %v", err )
}

func (jpg *Desc) getCurrentFrame() *frame {
    fl := len( jpg.frames )
    if fl > 0 {
        return &jpg.frames[fl-1]
    }
    return nil
}

func (jpg *Desc) getCurrentScan() *scan {
    if frm := jpg.getCurrentFrame(); frm != nil {
        if l := len( frm.scans ); l > 0 {
            return &frm.scans[l - 1]
        }
    }
    return nil
}

func (j *Desc)addSeg( seg segmenter ) {
    j.segments = append( j.segments, seg )
}
func (jpg *Desc)printMarker( marker, sLen, offset uint ) {
    if jpg.Markers {
        fmt.Printf( "Marker 0x%x, len %d, offset 0x%x (%s)\n",
                    marker, sLen, offset, getJPEGmarkerName(marker) )
    }
}

type Control struct {       // control parsing verbosity
    Warn            bool    // Warn about inconsistencies as they are seen
    Recurse         bool    // Recurse and parse embedded JPEG pictures
    TidyUp          bool    // Fix and clean up JPEG segments
    Markers         bool    // show JPEG markers as they are parsed
    Mcu             bool    // display MCUs as they are parsed
    Du              bool    // display each DU resulting from MCU parsing
    Begin, End      uint    // control MCU &DU display (from begin to end, included)
}

// Parse analyses jpeg data and splits the data into well-known segments.
// The argument toDo indicates how parsing should be done (Resurse) and what
// information should be printed during parsing (Warning, Markers, Mcu, Du).
// It can also request that possible errors be corrected and that unnecessary
// segments be removed (TidyUp).
//
// What can be corrected:
//
//  - if the last RSTn is ending a scan, it is not necessary and it may cause a
//  renderer to fail. It is removed from the scan.
//
//  - if a DNL table is found after an ECS and if the number of lines given in
//  the SOFn table was 0, the number of lines found in DNL is set in the SOFn
//  and in metadata and the DNL table is removed
//
//  - if the number of lines calculated from the scan data is different from
//  the SOFn value, the SOFn value and metadata are updated (this is done
//  after DNL processing).
//
// It returns a tuple: a pointer to a Desc containing segment definitions and
// and an error. In all cases, nil error or not, the returned Desc is usable
// (but wont be complete in case of error).
func Parse( data []byte, toDo *Control ) ( *Desc, error ) {

    jpg := new( Desc )   // initially in INIT state (0)
    jpg.Control = *toDo
    jpg.data = data

    if ! bytes.Equal( data[0:2],  []byte{ 0xff, 0xd8 } ) {
		return jpg, fmt.Errorf( "Parse: Wrong signature 0x%x for a JPEG file\n", data[0:2] )
	}

    tLen := uint(len(data))
makerLoop:
    for i := uint(0); i < tLen; {
        marker := uint(data[i]) << 8 + uint(data[i+1])
        sLen := uint(0)       // case of a segment without any data

        if marker < _TEM {
		    return jpg, fmt.Errorf( "Parse: invalid marker 0x%x\n", data[i:i+1] )
        }

        switch marker {

        case _SOI:            // no data, no length
            jpg.printMarker( marker, sLen, i )
            if jpg.state != _INIT {
		        return jpg, fmt.Errorf( "Parse: Wrong sequence %s in state %s\n",
                                        getJPEGmarkerName(marker), jpg.getJPEGStateName() )
            }
            jpg.state = _APPLICATION

        case _RST0, _RST1, _RST2, _RST3, _RST4, _RST5, _RST6, _RST7:
                                // empty segment, no following length
            jpg.printMarker( marker, sLen, i )
            return jpg, fmt.Errorf ("Parse: Marker %s should not happen in top level segments\n",
                                     getJPEGmarkerName(marker) )

        case _EOI:
            jpg.printMarker( marker, sLen, i )
            if jpg.state != _SCAN1 && jpg.state != _SCANn {
		        return jpg, fmt.Errorf( "Parse: Wrong sequence %s in state %s\n",
                            getJPEGmarkerName(marker), jpg.getJPEGStateName() )
            }
            jpg.state = _FINAL
            jpg.offset = i + 2  // points after the last byte
            if jpg.TidyUp { jpg.fixLines( ) }
            break makerLoop // exit even if there is junk at the end of the file

        default:        // all other cases have data following marker & length
            sLen = uint(data[i+2]) << 8 + uint(data[i+3])
            jpg.printMarker( marker, sLen, i )
            transitionToFrame := true
            var err error

            switch marker {    // second level marker switching within the first default
            case _APP0:
                err = jpg.app0( marker, sLen )
                transitionToFrame = false
            case _APP1:
                err = jpg.app1( marker, sLen )
                transitionToFrame = false

            case _APP2, _APP3, _APP4, _APP5, _APP6, _APP7, _APP8, _APP9,
                 _APP10, _APP11, _APP12, _APP13, _APP14, _APP15:
                transitionToFrame = false

            case _SOF0, _SOF1, _SOF2, _SOF3, _SOF5, _SOF6, _SOF7, _SOF9, _SOF10,
                 _SOF11, _SOF13, _SOF14, _SOF15:
                err = jpg.startOfFrame( marker, sLen )

            case _DHT:  // Define Huffman Table
                err = jpg.defineHuffmanTable( marker, sLen )

            case _DQT:  // Define Quantization Table
                err = jpg.defineQuantizationTable( marker, sLen )

            case _DAC:    // Define Arithmetic coding
                return jpg, fmt.Errorf( "Parse: Unsupported Arithmetic coding table %s\n",
                                        getJPEGmarkerName(marker) )

            case _DNL:
                err = jpg.defineNumberOfLines( marker, sLen )

            case  _DRI:  // Define Restart Interval
                err = jpg.defineRestartInterval( marker, sLen )

            case _SOS:
                err = jpg.processScan( marker, sLen )
                if err != nil { return jpg, jpgForwardError( "Parse", err ) }
                i = jpg.offset          // jpg.offset has been updated
                continue

            case _COM:  // Comment
                err = jpg.commentSegment( marker, sLen )

            case _DHP, _EXP:  // Define Hierarchical Progression, Expand reference components
                return jpg, fmt.Errorf( "Parse: Unsupported hierarchical table %s\n",
                                        getJPEGmarkerName(marker) )

            default:    // All JPEG extensions and reserved markers (_JPG, _TEM, _RESn)
                return jpg, fmt.Errorf( "Parse: Unsupported JPEG extension or reserved marker%s\n",
                                        getJPEGmarkerName(marker) )
            }
            if err != nil { return jpg, jpgForwardError( "Parse", err ) }
            if jpg.state == _APPLICATION && transitionToFrame {
                jpg.state = _FRAME
            }
        }
        i += sLen + 2
        jpg.offset = i          // always points at the mark
    }
    return jpg, nil
}

// IsComplete returns true if the current JPEG data makes a complete JPEG file,
// from SOI to EOI. It does not guarantee that the data corresponds to a valid
// JPEG image that can be used with any decoder.
func (jpg *Desc) IsComplete( ) bool {
    return jpg.state == _FINAL
}

// GetNumberOfFrames returns the number of frames in the file, which can be 0
// (no frame or parsing ended up in error), 1(most common case), or more in
// case of hierarchical frames.
func (jpg *Desc) GetNumberOfFrames( ) uint {
    return uint(len(jpg.frames))
}

// GetActualLengths returns the number of bytes between SOI and EOI (both
// included) in the possibly fixed jpeg data, and the original data length.
// The actual data length may be different from the original length if the
// analysis stopped in error, TidyUp has actually corrected some segment,
// RemoveMetadata have been called or if there is some garbage at the end of
// the original file that should be ignored.
func (jpg *Desc) GetActualLengths( ) ( actual uint, original uint ) {
    dataSize := uint( len( jpg.data ) )
    if ! jpg.IsComplete() { return 0, dataSize }
    size, err := jpg.serialize( ioutil.Discard )
    if err != nil {
        return 0, dataSize
    }
    return uint(size), dataSize
}

func (jpg *Desc) GetImageOrientation( ) (*Orientation, error) {
    if jpg.orientation == nil {
        return nil, fmt.Errorf( "GetImageOrientation: no orientation information\n" )
    }
    return jpg.orientation, nil
}

func make8BitComponentArrays( cmps []scanComp ) [](*[]uint8) {

    cArrays := make( [](*[]uint8), len( cmps ) ) // one flat []byte par component

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
    frm := jpg.frames[frame]
    sc := frm.scans[0]
    if sc.mcuD == nil || len(sc.mcuD.sComps) == 0 {
        return nil, fmt.Errorf( "MakeFrameRawPicture: no scan available for picture\n" )
    }

    cmps := sc.mcuD.sComps
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
func (jpg *Desc) writeBW( f *os.File, samples [](*[]uint8), sComps []scanComp,
                          o *Orientation ) (nc, nr uint, n int, err error) {

    Y := samples[0]
    yStride := sComps[0].nUnitsRow << 3

    bw := bufio.NewWriterSize( f, writeBufferSize )
    cbw := newCumulativeWriter( bw )

    writeBW := func( r, c uint ) {
        ys  := (*Y)[r*yStride+c]
        cbw.Write( []byte{ ys, ys, ys } )
    }

    var writeOrientedBW func()
    dLen  := uint(len(*Y))
    nRows := dLen / yStride

    if o == nil || (o.Row0 == Top && o.Col0 == Left ) { // default orientation
        nr = nRows
        nc = yStride
        writeOrientedBW = func() {
            for i := uint(0); i < dLen; i++ {
                writeBW( i / yStride, i % yStride )
            }
        }
    } else if o.Row0 == Top && o.Col0 == Right {
        nr = nRows
        nc = yStride
        cStart := yStride - 1
        writeOrientedBW = func () {
            for i := uint(0);i < dLen; i++ {
                writeBW( i / yStride, cStart - (i % yStride) )
            }
        }
    } else if o.Row0 == Right && o.Col0 == Top {        // rotation +90
        nr = yStride
        nc = nRows
        rStart := nRows - 1
        writeOrientedBW = func () {
            for i := uint(0);i < dLen; i++ {
                writeBW( rStart - (i % nRows), i / nRows )
            }
        }
    } else if o.Row0 == Right && o.Col0 == Bottom {
        nr = yStride
        nc = nRows
        rStart := nRows - 1
        cStart := yStride - 1
        writeOrientedBW = func () {
            for i := uint(0);i < dLen; i++ {
                writeBW( rStart - i % nRows, cStart - (i / nRows) )
            }
        }
    } else if o.Row0 == Bottom && o.Col0 == Left {
        nr = nRows
        nc = yStride
        rStart := nRows - 1
        writeOrientedBW = func () {
            for i := uint(0);i < dLen; i++ {
                writeBW( rStart - (i / yStride), i % yStride )
            }
        }
    } else if o.Row0 == Bottom && o.Col0 == Right {
        nr = nRows
        nc = yStride
        rStart := nRows - 1
        cStart := yStride - 1
        writeOrientedBW = func () {
            for i := uint(0);i < dLen; i++ {
                writeBW( rStart - (i / yStride), cStart - (i % yStride) )
            }
        }
    } else if o.Row0 == Left && o.Col0 == Top {
        nr = yStride
        nc = nRows
        writeOrientedBW = func() {
            for i := uint(0); i < dLen; i++ {
                writeBW( i % nRows, i / nRows )
            }
        }
    } else if o.Row0 == Left && o.Col0 == Bottom {      // rotation -90
        nr = yStride
        nc = nRows
        cStart := yStride - 1
        writeOrientedBW = func() {
            for i := uint(0); i < dLen; i++ {
                writeBW( i % nRows, cStart - (i / nRows) )
            }
        }
    }

    writeOrientedBW( )
    n, err = cbw.result()
    err = bw.Flush()
    return
}

func (jpg *Desc) writeYCbCr( f *os.File, samples [](*[]uint8), sComps []scanComp,
                             o *Orientation ) (nc, nr uint, n int, err error) {
    if len(samples) != 3 {
        panic("writeYCbCr: incorrect number of components\n")
    }

    Y := samples[0]
    Cb := samples[1]
    Cr := samples[2]

    yHSF := sComps[0].hSF
    yVSF := sComps[0].vSF
    yStride := sComps[0].nUnitsRow << 3 

    CbHSF := sComps[1].hSF
    CbVSF := sComps[1].vSF
    CbStride := sComps[1].nUnitsRow << 3 

    CrHSF := sComps[2].hSF
    CrVSF := sComps[2].vSF
    CrStride := sComps[2].nUnitsRow << 3 
//fmt.Printf("yHSF %d, CbHSF %d, CrHSF %d, yVSF %d, CbVSF %d, CrVSF %d, CbStride %d, CrStride %d\n",
//            yHSF, CbHSF, CrHSF, yVSF, CbVSF, CrVSF, CbStride, CrStride )
    bw := bufio.NewWriterSize( f, writeBufferSize )
    cbw := newCumulativeWriter( bw )

    // Assuming yHSF and yVSF are >= Cb/Cr H/V SF:
    // Destination is an array of packed RGB values, indexed by i [0..len[Y]]
    // Sources are Y, Cb and Cr arrays indexed such that given source row r and
    // col c, sample Ys is directly y[j] whereas samples Cbs and Crs are given
    // by C{b/r}s = Cb[((*rC{b/r}VSF)/yVSF)*CbStride + (c*C{b/r}HSF)/yHSF])
    // Depending on actual orientation (Row0 and Col0) the source row r and col
    // c are calculated from the destination index i

    writeRGB := func( r, c uint ) {
        ys  := float32((*Y)[r*yStride+c])
        Cbs := float32((*Cb)[((r*CbVSF)/yVSF)*CbStride + (c*CbHSF)/yHSF])
        Crs := float32((*Cr)[((r*CrVSF)/yVSF)*CrStride + (c*CrHSF)/yHSF])

        rs := int( 0.5 + ys + 1.402*(Crs-128.0) )
        if rs < 0 { rs = 0 } else if rs > 255 { rs = 255 }
        gs := int( 0.5 + ys - 0.34414*(Cbs-128.0) - 0.71414*(Crs-128.0) )
        if gs < 0 { gs = 0 } else if gs > 255 { gs = 255 }
        bs := int( 0.5 + ys + 1.772*(Cbs-128.0) )
        if bs < 0 { bs = 0 } else if bs > 255 { bs = 255 }

        cbw.Write( []byte{ byte(rs), byte(gs), byte(bs) } )
    }

    var writeOrientedRGB func()
    dLen  := uint(len(*Y))
    nRows := dLen / yStride

    if o == nil || (o.Row0 == Top && o.Col0 == Left ) { // default orientation
        nr = nRows
        nc = yStride
        writeOrientedRGB = func() {
            for i := uint(0); i < dLen; i++ {
                writeRGB( i / yStride, i % yStride )
            }
        }
    } else if o.Row0 == Top && o.Col0 == Right {
        nr = nRows
        nc = yStride
        cStart := yStride - 1
        writeOrientedRGB = func () {
            for i := uint(0);i < dLen; i++ {
                writeRGB( i / yStride, cStart - (i % yStride) )
            }
        }
    } else if o.Row0 == Right && o.Col0 == Top {        // rotation +90
        nr = yStride
        nc = nRows
        rStart := nRows - 1
        writeOrientedRGB = func () {
            for i := uint(0);i < dLen; i++ {
                writeRGB( rStart - (i % nRows), i / nRows )
            }
        }
    } else if o.Row0 == Right && o.Col0 == Bottom {
        nr = yStride
        nc = nRows
        rStart := nRows - 1
        cStart := yStride - 1
        writeOrientedRGB = func () {
            for i := uint(0);i < dLen; i++ {
                writeRGB( rStart - i % nRows, cStart - (i / nRows) )
            }
        }
    } else if o.Row0 == Bottom && o.Col0 == Left {
        nr = nRows
        nc = yStride
        rStart := nRows - 1
        writeOrientedRGB = func () {
            for i := uint(0);i < dLen; i++ {
                writeRGB( rStart - (i / yStride), i % yStride )
            }
        }
    } else if o.Row0 == Bottom && o.Col0 == Right {
        nr = nRows
        nc = yStride
        rStart := nRows - 1
        cStart := yStride - 1
        writeOrientedRGB = func () {
            for i := uint(0);i < dLen; i++ {
                writeRGB( rStart - (i / yStride), cStart - (i % yStride) )
            }
        }
    } else if o.Row0 == Left && o.Col0 == Top {
        nr = yStride
        nc = nRows
        writeOrientedRGB = func() {
            for i := uint(0); i < dLen; i++ {
                writeRGB( i % nRows, i / nRows )
            }
        }
    } else if o.Row0 == Left && o.Col0 == Bottom {      // rotation -90
        nr = yStride
        nc = nRows
        cStart := yStride - 1
        writeOrientedRGB = func() {
            for i := uint(0); i < dLen; i++ {
                writeRGB( i % nRows, cStart - (i / nRows) )
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
        return 0, 0, 0, fmt.Errorf( "SaveRawPicture: multiple framre are not supported\n" )
    }
    frm := jpg.frames[0]
    sc := frm.scans[0]
    if sc.mcuD == nil || len(sc.mcuD.sComps) == 0 {
        return 0, 0, 0, fmt.Errorf( "SaveRawPicture: no scan available for picture\n" )
    }

    cmps := sc.mcuD.sComps
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
            nCols, nRows, n, err = jpg.writeYCbCr( f, samples, cmps, ort )
            break
        }
        fallthrough
    case 1: nCols, nRows, n, err = jpg.writeBW( f, samples, cmps, ort )
    default:
        err = fmt.Errorf("SaveRawPicture: not YCbCr or Gray scale picture\n")
    }
    return
}

//  RemoveMetadata removes metadata:
//  a first id (appId) specifies the app segment containing metadata (-1 for all
//  apps, or a list of specific app ids to remove, in the the range 0 to 15).
//  The following list of ids indicates which containers inside the app segment
//  to remove. This is intended for app segments, such as app1 used for EXIF,
//  which contains up to 6 ifds, in the range 1 to 6. If that list of ids is
//  missing the whole app segment is removed.
func (jpg *Desc)RemoveMetadata( appId int, sIds []int ) (err error) {
    for _, seg := range jpg.segments {
        if s, ok := seg.(metadata); ok {
            err = s.mRemove( appId, sIds )
            if err != nil {
                break
            }
        }
    }
    return
}

type ThumbSpec struct {         // argument to SaveThumbnail
    Path    string              // new thumbnail file path
    ThId    int                 // thumbnail id
}

// SaveThumbnail save the embedded thumbnail(s) in separate files. The argument
// is a list of thumbSpec (a path to the  new file and the thumbnail id to
// extract). Pictures usually embed a thumbnail image and in some cases a
// second image, sometimes called a preview image. By convention thumbnail id
// 0 refers to the main thumbnail and id 1 to the second image.
//
// Note however that if multiple app segments can provide thumbnails, and a
// first one in the JPEG file does not include the requested thumbnail the call
// fails and does not try any other app segment that might be able to provide
// the thumbnail. If a first app segment provides the requested thumbnails, the
// following app segments are not used.
func (jpg *Desc)SaveThumbnail( tspec []ThumbSpec ) (err error) {
segLoop:
    for _, seg := range jpg.segments {
        if s, ok := seg.(metadata); ok {
            collected := 0
            for _, t := range tspec {
                var n int
                n, err = s.mThumbnail( t.ThId, t.Path )

                if err != nil {
                    break segLoop
                }
                if n == 0 {     // app segment does not provide thumbnails
                    break       // try anothe app
                }
                collected ++
            }
            if collected == len( tspec ) {
                break           // thumbnails have been collected: stop here
            }
        }
    }
    return
}

func (jpg *Desc)serialize( w io.Writer ) (n int, err error) {

    if n, err = w.Write( []byte{ 0xFF, 0xD8 } ); err == nil {
        var ns int
        for _, s := range jpg.segments {
            ns, err = s.serialize( w ); if err != nil {
                return
            }
            n += ns
        }
        if ns, err = w.Write( []byte{ 0xFF, 0xD9 } ); err == nil {
            n += ns
        }
    }
    return
}

// FormatSegments prints out all segments that constitute the image.
func (jpg *Desc) FormatSegments( w io.Writer ) (n int, err error) {
    var np int
    for _, s := range jpg.segments {
        np, err = s.format( w )
        if err != nil {
            return
        }
        n += np
    }
    return
}

// Generate returns a copy in memory of the possibly fixed jpeg file after analysis.
func (jpg *Desc) Generate( ) ( []byte, error ) {
    var b bytes.Buffer
    _, err := jpg.serialize( &b )
    if  err != nil { return nil, fmt.Errorf( "Generate: ", err ) }
    return b.Bytes(), nil
}

// Write stores the possibly fixed JEPG data into a file.
// The argument path is the new file path.
// If the file exists already, new content will replace the existing one.
func (jpg *Desc)Write( path string ) (n int, err error) {
    if ! jpg.IsComplete() {
        return 0, fmt.Errorf( "Write: Data is not a complete JPEG\n" )
    }

    defer func ( ) { if err != nil { err = jpgForwardError( "Write", err ) } }()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.ModePerm)
    if err == nil {
        defer func ( ) {
            if e := f.Close( ); err == nil {
                err = e // replace with close error only if no previous error
            }
        }()
        n, err = jpg.serialize( f )
    }
    return
}

/*
    Read reads a JPEG file in memory, and parses its content. The argument path
    is the existing file path. The argument toDo provides information about how
    to analyse the document. If toDo.TidyUp is true, Read fixes some common
    issues in jpeg data by updating data in memory, so that they can be stored
    later by calling Write or Generate.

    It returns a tuple: a pointer to a Desc containing the segment
    definitions and an error. If the file cannot be read the returned Desc
    is nil.
*/
func Read( path string, toDo *Control ) ( *Desc, error ) {
    data, err := ioutil.ReadFile( path )
    if err != nil {
		return nil, fmt.Errorf( "ReadJpeg: Unable to read file %s: %v\n", path, err )
	}
    return Parse( data, toDo )
}

