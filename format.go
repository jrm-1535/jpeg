package jpeg

import (
    "fmt"
    "io"
)

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

// GetImageInfo returns the framing information, whether it is a single frame
// (sequential or progressive) or multiple frames (hierarchical)
func (j *Desc)GetImageInfo( ) Framing {
    return j.process
}
// FormatImageInfo formats and writes the image framing information, whether
// it is  a single frame (sequential or progressive) or multiple frames
// (hierarchical)
func (j *Desc)FormatImageInfo( w io.Writer ) (n int, err error) {
    if j.process == HierarchicalFrames {
        n, err = fmt.Fprintf( w, "Image Info: %d Hierarchical Frames\n",
                             len(j.frames) )
    } else {
        n, err = w.Write( []byte("Image Info: Single Frame\n") )
    }
    return
}

func (j *Desc)getFrameSegment( fi uint ) *frame {
    if fi < 0 || fi >= uint(len(j.frames)) {
        return nil
    }
    return &j.frames[fi]
}

type Component struct {
    Id, HSF, VSF, QS uint8
}

type FrameInfo struct {
    Mode            EncodingMode    // baseline, sequential, progressive, lossless
    Entropy         EntropyCoding   // Huffman or arithmetic coding
    SampleSize      uint            // number of bits per pixel
    Width, Height   uint            // image size in pixels
    Components      []Component     // frame components
}

// GetFrameInfo returns encoding information about a specific frame, indentified
// by the argument frame. An error is returned if the requested frame does not
// exist. For non-hierarchical modes, only one frame (0) is used.
func (j *Desc)GetFrameInfo( fi uint ) (*FrameInfo, error) {
    frm := j.getFrameSegment( fi )
    if frm == nil {
        return nil, fmt.Errorf( "GetFrameInfo: frame %d is absent\n", fi )
    }

    finfo := new (FrameInfo)
    finfo.Mode = frm.encodingMode( )
    finfo.Entropy = frm.entropyCoding( )
    finfo.SampleSize = frm.samplePrecision( )
    finfo.Width = frm.nSamplesLine( )
    finfo.Height = uint(frm.actualLines( ))

    finfo.Components = make( []Component, len(frm.components) )
    for i, cmp := range frm.components {
        finfo.Components[i].Id = cmp.Id
        finfo.Components[i].Id = cmp.HSF
        finfo.Components[i].Id = cmp.VSF
        finfo.Components[i].Id = cmp.QS
    }
    return finfo, nil
}

// FormatFrameInfo writes a textual description of a specific frame encoding
// information. An error is returned if the requested frame does not exist.
// For non-hierarchical modes, only one frame (0) is used.
func (j *Desc)FormatFrameInfo( w io.Writer, fi uint ) (n int, err error) {
    frm := j.getFrameSegment( fi )
    if frm == nil {
        return 0, fmt.Errorf( "FormatFrameInfo: frame %d is absent\n", fi )
    }
    if n, err = fmt.Fprintf( w, "Frame #%d:\n", frm.id ); err != nil {
        return
    }
    var np int
    np, err = frm.format( w )
    n += np
    return
}

func (j *Desc)getFrameSegmentIndex( n uint ) int {

    for i, s := range j.segments {
        if _, ok := s.(*frame); ok {
            if n == 0 {
                return i
            }
            n --
        }
    }
    return -1
}

func (j *Desc)getStartOfScanSegmentIndex( fi int ) int {
    for i, s := range j.segments[fi:] {
        if _, ok := s.(*scan); ok {
            return i
        }
    }
    return -1
}

func (j *Desc)getDefineHuffmanSegmentIndex( fi int ) int {
    for i, s := range j.segments[fi:] {
        if _, ok := s.(*htSeg); ok {
            return i
        }
    }
    return -1
}

func (j *Desc)getQuantizationSegmentsForFrame( n uint ) ([]*qtSeg, error) {
    var first, beyond int
    if n > 0 {
        first = j.getFrameSegmentIndex( n )
        if first < 0 {
            return nil, fmt.Errorf( "getQuantizationSegmentsForFrame: frame %d is absent\n", n )
        }
    } else {
        first = 0
    }

    beyond = j.getStartOfScanSegmentIndex( first )
    if beyond == -1 {
        return nil, fmt.Errorf( "getQuantizationSegmentsForFrame: no SOS for frame %d\n", n )
    }

    var qts []*qtSeg
    for _, s := range j.segments[first:beyond] {
        if qt, ok := s.(*qtSeg); ok {
            qts = append( qts, qt )
        }
    }
    return qts, nil
}

func (j *Desc)formatQuantization( w io.Writer, fr uint, dest int,
                                  m FormatMode, skip bool ) (n int, err error) {

    type qtindex struct{ qt *qtSeg; index int }
    qts, err := j.getQuantizationSegmentsForFrame( fr )
    if err != nil {
        return 0, fmt.Errorf( "formatQuantization: %v", err )
    }
//fmt.Printf( "frame %d quant index %d skip %v n quant %d\n", fr, q, skip, len(qts) )
    var qtindexes []qtindex
    if dest != -1 {
        for _, qt := range qts {
            start := 0
            for {
                start = qt.matchDestination( start, uint(dest) )
                if start == -1 {
                    break;
                }
                qtindexes = append( qtindexes, qtindex{ qt, start } )
                start ++
            }
        }
        if ! skip && len(qtindexes) == 0 {
            return 0, fmt.Errorf( "formatQuantization: destination %d not used\n",
                                  dest )
        }
    }

    cw := newCumulativeWriter( w )
    cw.format( "Frame #%d\n", fr )

    if dest == -1 {
        for _, qt := range qts {
            qt.formatAllDest( cw, m )
        }
    } else {
        for _, qtindex := range qtindexes {
            qtindex.qt.formatDestAt( cw, qtindex.index, m )
        }
    }
    n, err = cw.result()
    return
}

func (j *Desc)formatQuantizationSegment( w io.Writer, frame uint, d int,
                                         m FormatMode ) (int, error) {
    if d > 3 || d < -1 {
        return 0, fmt.Errorf("formatQuantizationTable: destination %d is" +
                             "out of range\n", d)
    }
    return j.formatQuantization( w, frame, d, m, false )
}

func (j *Desc)getHuffmanSegmentsForFrame( n uint ) ([]*htSeg, error) {
    var first, beyond int
    if n > 0 {
        first = j.getFrameSegmentIndex( n )
        if first < 0 {
            return nil, fmt.Errorf( "getHuffmanSegmentsForFrame: frame %d is absent\n", n )
        }
    } else {
        first = 0
    }

    beyond = j.getStartOfScanSegmentIndex( first )
    if beyond == -1 {
        return nil, fmt.Errorf( "getHuffmanSegmentsForFrame: no SOS for frame %d\n", n )
    }
//fmt.Printf("frame %d first %d beyond %d\n", n, first, beyond )
    var hts []*htSeg
    for _, s := range j.segments[first:beyond] {
        if ht, ok := s.(*htSeg); ok {
            hts = append( hts, ht )
        }
    }
    return hts, nil
}

func (j *Desc)formatHuffmanEntropy( w io.Writer, fr uint, dest int,
                                    m FormatMode, skip bool ) (n int, err error) {
    type htindex struct{ ht *htSeg; index int }
    hts, err := j.getHuffmanSegmentsForFrame( fr )
    if err != nil {
        return 0, fmt.Errorf( "formatHuffmanEntropy: %v\n", err )
    }
//fmt.Printf( "frame %d huffman destination %d skip %v n dest %d\n", fr, dest, skip, len(hts) )
    var htindexes []htindex
    if dest != -1 {
        hc := byte(dest / 4)
        hd := byte(dest % 2)
        for _, ht := range hts {
            start := 0
            for {
                start = ht.matchClassDestination( start, hc, hd )
                if start == -1 {
                    break;
                }
                htindexes = append( htindexes, htindex{ ht, start } )
                start ++
            }
        }
        if ! skip && len(htindexes) == 0 {
            return 0, fmt.Errorf( "formatHuffmanEntropy: destination %d not used\n",
                                  dest )
        }
    }

    cw := newCumulativeWriter( w )
    cw.format( "Frame #%d\n  Entropy: Huffman Coding\n", fr )

    if dest == -1 {
        for _, ht := range hts {
            ht.formatAllDest( cw, m )
        }
    } else {
        for _, htindex := range htindexes {
            htindex.ht.formatDestAt( cw, htindex.index, m )
        }
    }
    n, err = cw.result()
    return
}

func (j *Desc)formatArithmeticEntropy( w io.Writer, f uint, d int,
                                       m FormatMode, skip bool ) (int, error) {

    return fmt.Fprintf( w, "Frame #%d\n  Entropy: Arithmetic Coding\n" +
                        "  Not supported yet\n", f )
}

func (j *Desc)formatEntropySegment( w io.Writer, frame uint,
                                    dest int, mode FormatMode ) (int, error) {
    frs := j.getFrameSegment( frame )
    if frs == nil {
        return 0, fmt.Errorf( "formatEntropySegment: frame %d is absent\n",
                              frame )
    }
    if dest > 7 || dest < -1 {
        return 0, fmt.Errorf("formatEntropySegment: destination %d is" +
                             "out of range\n", dest)
    }
    switch frs.entropyCoding( ) {
    case HuffmanCoding:
        return j.formatHuffmanEntropy( w, frame, dest, mode ,false )
    case ArithmeticCoding:
        return j.formatArithmeticEntropy( w, frame, dest, mode, false )
    default:
        panic( "formatEntropySegment: illegal entropy type\n" )
    }
}

func (j *Desc)formatScanSegment( w io.Writer, frame uint, index int,
                                 mode FormatMode ) (n int, err error) {
    frm := j.getFrameSegment( frame )
    if frm == nil {
        return 0, fmt.Errorf( "formatEntropySegment: frame %d is absent\n",
                              frame )
    }
    scs := frm.scans
    if index >= len(frm.scans) {
        return 0, fmt.Errorf( "formatEntropySegment: scan %s does not exist for frame %d\n",
                              index, frame )
    }
    cw := newCumulativeWriter( w )
    cw.format( "Frame #%d\n", frame )

    if index == -1 {
        for i, sc := range scs {
            sc.formatAt( cw, i, mode )
        }
    } else {
        sc := frm.scans[index]
        sc.formatAt( cw, index, mode )
    }
    n, err = cw.result()
    return
}

type EncodingTable int
const (
    Quantization EncodingTable = iota
    Entropy
    Scan
)

type FormatMode int
const (
    Standard FormatMode = iota
    Extra
    Both
)

// FormatEncodingTable formats and writes the requested encoding table for a
// given frame. In case of hierarchical frames, the frame number gives the
// frame for which an encoding table must be formatted, otherwise the only
// available frame is 0. It can be given as -1 to indicate the sequence of
// all frames in the picture.
//
// Encoding tables are Quantization tables, entroypy tables (Huffman coding
// tables or Arithmetic coding tables) and Scan tables (entropy coded segments).
//
// Since usually multiple tables exists, an index n is used to specify which
// table must be processed. An index of -1 indicates all tables in sequence.
//
// For each encoding table class (Quantization, Entropy or Scan) the sequence
// is defined as:
//
// Quantization: n = destination [0-3] DCT coefficients
//
// Entropy: n = class DC [0-3], class AC [4-7], either Huffman or Arithmetic
// coding
//
// Scan: n = scan segment
//
// Formatting those tables is done accoding to the requested mode (Standard,
// Extra or Both).
//
// An error is returned if the requested frame or table does not exist.
func (j *Desc)FormatEncodingTable( w io.Writer, frame uint, t EncodingTable,
                                   n int, mode FormatMode ) (int, error) {
    if mode < Standard || mode > Both {
        return 0, fmt.Errorf( "FormatEncodingTable: invalid format mode %d\n",
                              mode )
    }
    switch t {
    case Quantization:
        return j.formatQuantizationSegment( w, frame, n, mode )
    case Entropy:
        return j.formatEntropySegment( w, frame, n, mode )
    case Scan:
        return j.formatScanSegment( w, frame, n, mode )
    }
    return 0, fmt.Errorf( "FormatEncodingTable: unknown table %d\n", t )
}

// formatMetadata formats and writes the requested metadata for a given appId.
// The optional slice of sub-ids is intended for cases where the app segment
// contains multiple containers associated with those sub-ids, such as app1
// used for EXIF. The slice gives the specific containers to write. If the
// slice is missing the whole app segment is written.
func (j *Desc)FormatMetadata( w io.Writer, appId int, sIds []int ) (n int, err error) {
    for _, seg := range j.segments {
        if s, ok := seg.(metadata); ok {
            n, err = s.mFormat( w, appId, sIds )
            if err != nil || n != 0 { // stop if error or something was written
                break
            }
        }
    }
    return
}

func (j *Desc)FormatFrameComponent( w io.Writer,
                                    frame uint, comp int ) (n int, err error) {
    frm := j.getFrameSegment( frame )
    if frm == nil {
        return 0, fmt.Errorf( "FormatFrameComponent: frame %d is absent\n",
                              frame )
    }

    if comp >= len(frm.components) || comp < -1 {
        return 0, fmt.Errorf( "FormatFrameComponent: component %d not available\n",
                              comp )
    }

    cw := newCumulativeWriter( w )
    cw.format( "Frame %d, %d components:\n", frame, len(frm.components) )

    formatComponent := func( c int ) {
        cmp := &frm.components[c]
        cw.format( "  Component %d: allocated %d rows of %d samples\n",
                    c, len(cmp.iDCTdata) * 8, len(cmp.iDCTdata[0]) * 8 );
        vAlign := uint16(8 * frm.resolution.mvSF / cmp.VSF)
        auRows := ((frm.resolution.nLines + vAlign - 1) / vAlign) << 3
        hAlign := uint16(8 * frm.resolution.mhSF / cmp.HSF)
        auSamplesRow := ((frm.resolution.nSamplesLine + hAlign - 1) / hAlign) << 3
        cw.format( "           actually used %d rows of %d samples\n",
                    auRows, auSamplesRow)
    }

    if comp == -1 {
        for i, _ := range frm.components {
            formatComponent( i )
        }
    } else {
        formatComponent( comp )
    }
    n, err = cw.result()
/*
    var du = dataUnit{
        -416,  -33,  -60,   32,   48,  -40,     0,     0,
           0,  -24,  -56,   19,   26,    0,     0,     0,
         -42,   13,   80,  -24,  -40,    0,     0,     0,
         -42,   17,   44,  -29,    0,    0,     0,     0,
          18,    0,    0,    0,    0,    0,     0,     0,
           0,    0,    0,    0,    0,    0,     0,     0,
           0,    0,    0,    0,    0,    0,     0,     0,
           0,    0,    0,    0,    0,    0,     0,     0 }
    fmt.Printf("Source:\n%v\n", du )
    array := make( []uint8, 64 )
    inverseDCT8( &du, array, 8 )
    fmt.Printf("Inverse:\n%v\n", array )
*/
    return
}

