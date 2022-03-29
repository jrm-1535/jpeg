package jpeg

// support for JPEG app0 (JFIF)

import (
    "fmt"
    "bytes"
    "encoding/binary"
    "github.com/jrm-1535/exif"
    "io"
)

// metadata interface for all apps
type metadata interface {
    mFormat( w io.Writer, mid int, sids []int ) (int, error)
    mRemove( appId int, sId []int ) error
    mThumbnail( tid int, path string ) (int, error)
//    mExtract( mid int,  ) (int, error)
}

// app0 support

const (                             // Image resolution units (prefixed with _ to avoid being documented)
    _DOTS_PER_ARBITRARY_UNIT = 0    // undefined unit
    _DOTS_PER_INCH = 1              // DPI
    _DOTS_PER_CM = 2                // DPCM Dots per centimeter
)

func getUnitsString( units uint8 ) (string, string) {
    switch units {
    case _DOTS_PER_ARBITRARY_UNIT: return "dots per abitrary unit", "dp?"
    case _DOTS_PER_INCH:           return "dots per inch", "dpi"
    case _DOTS_PER_CM:             return "dots per centimeter", "dpcm"
    }
    return "Unknown units", ""
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
    _JFIF_BASE          = 0     // code for main JFIF app segment
    _THUMBNAIL_BASELINE = 0x10
    _THUMBNAIL_PALETTE  = 0x11
    _THUMBNAIL_RGB      = 0x12
)

const (
    _JFIF_FIXED_SIZE    = 16
    _JFXX_FIXED_SIZE    = 8
    _RGB_PIXEL_SIZE     = 3
    _PALETTE_SIZE       = _RGB_PIXEL_SIZE*256
)

type app0   struct {
    sType    uint8
    major,
    minor    uint8
    unit     uint8
    hDensity,
    vDensity uint16
    htNail,
    vtNail   uint8
    removed  bool
    thbnail  []byte
}

func (a0 *app0)serialize( w io.Writer ) (int, error) {
    if a0.removed {
        return 0, nil
    }
    var seg []byte
    switch a0.sType {
    case _JFIF_BASE:
        size := _JFIF_FIXED_SIZE +
                ( _RGB_PIXEL_SIZE * int(a0.htNail) * int(a0.vtNail) )
        seg = make( []byte, 2 + size )

        binary.BigEndian.PutUint16( seg, _APP0 )
        binary.BigEndian.PutUint16( seg[2:], uint16(size) )
        copy( seg[4:], "JFIF\x00" )

        seg[9] = a0.major
        seg[10] = a0.minor
        seg[11] = a0.unit

        binary.BigEndian.PutUint16(seg[12:], a0.hDensity)
        binary.BigEndian.PutUint16(seg[14:], a0.vDensity)
        seg[16] = a0.htNail
        seg[17] = a0.vtNail

        copy( seg[18:], a0.thbnail )

    case _THUMBNAIL_BASELINE:
        size := _JFXX_FIXED_SIZE + len(a0.thbnail)
        seg = make( []byte, 2 + size )

        binary.BigEndian.PutUint16( seg, _APP0 )
        binary.BigEndian.PutUint16( seg[2:], uint16(size) )
        copy( seg[4:], "JFXX\x00" )

        seg[9] = a0.sType
        copy( seg[10:], a0.thbnail )

    case _THUMBNAIL_PALETTE, _THUMBNAIL_RGB:
        size := _JFXX_FIXED_SIZE + 2 + len(a0.thbnail)
        seg = make( []byte, 2 + size )

        binary.BigEndian.PutUint16( seg, _APP0 )
        binary.BigEndian.PutUint16( seg[2:], uint16(size) )
        copy( seg[4:], "JFXX\x00" )

        seg[9] = a0.sType
        seg[10] = a0.htNail
        seg[11] = a0.vtNail
        copy( seg[12:], a0.thbnail )
    }
    return w.Write( seg )
}

func (a0 *app0)commonFormat( w io.Writer ) (int, error) {
    cw := newCumulativeWriter( w )
    switch a0.sType {
    case _THUMBNAIL_BASELINE:
        cw.format( "APP0  Extension:\n  Thumbnail coded using JPEG\n" )
    case _THUMBNAIL_PALETTE:
        cw.format( "APP0  Extension:\n  Thumbnail coded using 8-bit Palette\n" )
    case _THUMBNAIL_RGB:
        cw.format( "APP0  Extension:\n  Thumbnail coded using uncompressed 24-bit RGB\n" )
    case _JFIF_BASE:
        cw.format( "APP0:\n  JFIF Version %d.%02d\n", a0.major, a0.minor )
        units, symb := getUnitsString( a0.unit )
        cw.format( "  density in %s (%s)\n", units, symb )
        cw.format( "  density %d,%d %s\n", a0.hDensity, a0.vDensity, symb )
        cw.format( "  thumbnail %d,%d pixels\n", a0.htNail, a0.vtNail )
    default:
        panic("format app0: not a valid app\n")
    }
    return cw.result()
}
func (a0 *app0)format( w io.Writer ) (int, error) {
    return a0.commonFormat( w )
}

func (a0 *app0)mFormat( w io.Writer, appId int, sIds []int ) (int, error) {
    if appId == 0 {
        return a0.commonFormat( w )
    }
    return 0, nil
}

func (a0 *app0)mRemove( appId int, sId []int ) (err error) {
    if appId != 1 {
        return
    }
    a0.removed = true
    return
}

func (a0 *app0)mThumbnail( tid int, path string ) (n int, err error) {
    return
}

func (jpg *Desc) app0( marker, sLen uint ) error {
    if sLen < 8 {
        return fmt.Errorf( "app0: Wrong APP0 (JFIF) header (invalid length %d)\n", sLen )
    }
    if jpg.state != _APPLICATION {
        return fmt.Errorf( "app0: Wrong sequence %s in state %s\n",
                           getJPEGmarkerName(_APP0), jpg.getJPEGStateName() )
    }
    offset := jpg.offset + 4    // points 1 byte after length
    appType := markerAPP0discriminator( jpg.data[offset:offset+5] )
    if appType == -1 {
        return fmt.Errorf( "app0: Wrong APP0 header (%s)\n", jpg.data[offset:offset+4] )
    }

    if appType == _APP0_JFIF {
        if len(jpg.segments) != 0 {
            return fmt.Errorf( "app0: JFIF is not the first segment\n" )
        }
        if sLen < 16 {
            return fmt.Errorf( "app0: Wrong JFIF header (invalid length %d)\n", sLen )
        }
        htNail := jpg.data[offset+12]
        vtNail := jpg.data[offset+13]
        thbnSize := _RGB_PIXEL_SIZE * uint(htNail) * uint(vtNail)
        if sLen != _JFIF_FIXED_SIZE + thbnSize {
            return fmt.Errorf( "app0: Wrong JFIF header (incorrect len %d)\n", sLen )
        }

        a := new(app0)
        a.sType = _JFIF_BASE
        a.htNail = htNail
        a.vtNail = vtNail
        a.major = jpg.data[offset+5]    // 0x01
        a.minor = jpg.data[offset+6]    // 0x02
        a.unit = jpg.data[offset+7]
        a.hDensity = uint16( jpg.data[offset+8] ) << 8 + uint16( jpg.data[offset+9] )
        a.vDensity = uint16( jpg.data[offset+10] ) << 8 + uint16( jpg.data[offset+11] )
        if thbnSize != 0 {
            a.thbnail = make( []byte, thbnSize )
            copy( a.thbnail, jpg.data[offset+14:] )
        }
        jpg.addSeg( a )
//        jpg.addApp( a )
    } else {
        if len(jpg.segments) != 1 {
            return fmt.Errorf( "app0: JFIF extension does not follow JFIF\n" )
        }
        if jpg.app0Extension {
            return fmt.Errorf( "app0: Multiple JFIF extensions\n" )
        }

        a := new(app0)
        a.sType = jpg.data[offset+5]
        switch a.sType {
        case _THUMBNAIL_BASELINE:
            a.thbnail = make( []byte, sLen - 8 )   // Thumbnail JPEG file
            copy( a.thbnail, jpg.data[offset+6:] )
        case _THUMBNAIL_PALETTE:
            a.htNail = jpg.data[offset+6]
            a.vtNail = jpg.data[offset+7]
            thbnSize := _PALETTE_SIZE + (uint(a.htNail) * uint(a.vtNail))
            a.thbnail = make( []byte, thbnSize )
            copy( a.thbnail, jpg.data[offset+8:] )
        case _THUMBNAIL_RGB:
            a.htNail = jpg.data[offset+6]
            a.vtNail = jpg.data[offset+7]
            thbnSize := _RGB_PIXEL_SIZE * uint(a.htNail) * uint(a.vtNail)
            a.thbnail = make( []byte, thbnSize )
            copy( a.thbnail, jpg.data[offset+8:] )
        }
        jpg.app0Extension = true
        jpg.addSeg( a )
    }
    return nil
}

// app1 support (Exif, XMP)

const (
    _APP1_EXIF = iota
    _APP1_XMP
)

func (jpg *Desc) xmpApplication( offset, sLen uint ) error {
/*
    fmt.Printf( "APP1 (XMP)\n" )
    fmt.Printf( "  ----------------- Length %d -----------------\n", sLen )
// TODO: format XML
    fmt.Printf( "%s\n", string(jpg.data[jpg.offset+33:jpg.offset+4+sLen]) )
    fmt.Printf( "  --------------------------------------------------\n" )
*/
    return nil
}

type exifData struct {
    removed bool
    desc *exif.Desc
}

func (ed *exifData) serialize( w io.Writer) (n int, err error) {
    if ed.removed {
        return
    }
    var sz int
    if sz, err = ed.desc.Serialize( io.Discard ); err != nil {
        return
    }
    seg := make( []byte, 4 )
    binary.BigEndian.PutUint16( seg, _APP1 )
    binary.BigEndian.PutUint16( seg[2:], uint16(sz+2) )

    cw := newCumulativeWriter( w )
    cw.Write( seg )
    ed.desc.Serialize( cw )
    n, err = cw.result()
    return
}

func (ed *exifData)format( w io.Writer) (n int, err error) {
    cw := newCumulativeWriter( w )
    ed.desc.Format( cw )
    n, err = cw.result()
    if err != nil { err = fmt.Errorf( "format: %w", err ) }
    return
}

func (ed *exifData)mFormat( w io.Writer, appId int, sIds []int ) (int, error) {
    if appId == 1 {
        if len(sIds) == 0 {
            return ed.desc.Format( w )
        }
        args := make( []exif.IfdId, len(sIds) )
        for i, sId := range sIds {
            args[i] = exif.IfdId(sId)
        }
        return ed.desc.FormatIfds( w, args )
    }
    return 0, nil
}

func (ed *exifData)mRemove( appId int, sId []int ) (err error) {
    if appId != 1 {
        return
    }
    if len(sId) == 0 {
        ed.removed = true
        return
    }
    for _, id := range sId {
        if id == 0 {
            ed.removed = true
            break
        } else {
            err = ed.desc.Remove( exif.IfdId(id), -1 )
            if err != nil {
                break
            }
        }
    }
    return
}

func (ed *exifData) mThumbnail( tid int, path string ) (n int, err error) {
    var from exif.IfdId
    if tid == 0 {
        from = exif.THUMBNAIL
    } else if tid == 1 {
        from = exif.EMBEDDED
    } else {
        err = fmt.Errorf( "mThumbnail: invalid thumbnail id: %d\n", tid )
        return
    }
    n, err = ed.desc.WriteThumbnail( path, from )
    return
}


func (ed *exifData)parseThumbnails( ) (err error) {

    var toClose bool
    eThbns := ed.desc.GetThumbnailInfo()

    defer func( ) {
        if err != nil { err = fmt.Errorf( "parseThumbnails: %v", err ) }
    }()
    for i, thbn := range eThbns {
        fmt.Printf( "Thumbnail #%d: type %s, size %d in %s IFD\n",
                    i, exif.GetCompressionName(thbn.Comp),
                    thbn.Size, exif.GetIfdName(thbn.Origin) )

        if thbn.Comp == exif.JPEG {   // decode thumbnail if in JPEG
            var data []byte
            data, err = ed.desc.GetThumbnailData( thbn.Origin );
            if err != nil {
                return
            }
            fmt.Printf( "============= Thumbnail JPEG picture ================\n" )
            toClose = true
            _, err = Parse( data, &Control{ Markers: true } )
            if err != nil {
                return
            }
        }
    }
    if toClose {
        fmt.Printf( "================== Main JPEG picture ==================\n" )
    }
    return nil
}

func (jpg *Desc) setTiffOrientation( ed *exifData ) {
    const tiffOrientation = 0x112

    if jpg.orientation != nil {
        if jpg.orientation.AppSource == 1 {
            return  // keep previous orientation
        }
    }
    d := ed.desc
    st, v, err := d.GetIfdTagValue( exif.PRIMARY, tiffOrientation )
    if err != nil {
        return      // no ifd?
    }
    if st != exif.U16Slice {
        return      // not usable
    }
    slu16 := v.([]uint16)
    if len(slu16) != 1 {
        return
    }
    ocode := slu16[0]
    orientation := new(Orientation)
    switch ocode {
    default:
        return
    case 1:
        orientation.Row0 = Top
        orientation.Col0 = Left
        orientation.Effect = None
    case 2:
        orientation.Row0 = Top
        orientation.Col0 = Right
        orientation.Effect = VerticalMirror
    case 3:
        orientation.Row0 = Bottom
        orientation.Col0 = Right
        orientation.Effect = Rotate180
    case 4:
        orientation.Row0 = Bottom
        orientation.Col0 = Left
        orientation.Effect = HorizontalMirror
    case 5:
        orientation.Row0 = Left
        orientation.Col0 = Top
        orientation.Effect = HorizontalMirrorRotate90
    case 6:
        orientation.Row0 = Right
        orientation.Col0 = Top
        orientation.Effect = Rotate90
    case 7:
        orientation.Row0 = Right
        orientation.Col0 = Bottom
        orientation.Effect = VerticalMirrorRotate90
    case 8:
        orientation.Row0 = Left
        orientation.Col0 = Bottom
        orientation.Effect = Rotate270
    }
    orientation.AppSource = 1
    jpg.orientation = orientation
}

func (jpg *Desc) exifApplication( offset, sLen uint ) error {
    ec := exif.Control{ Unknown: exif.KeepTag, Warn: true }
    d, err := exif.Parse( jpg.data, offset, sLen, &ec )

    if err == nil {
        ed := new(exifData)
        ed.desc = d
        jpg.addSeg( ed )
        jpg.setTiffOrientation( ed )

        if jpg.Recurse {
            if err = ed.parseThumbnails(); err != nil {
                return fmt.Errorf( "exifApplication: %v", err )
            }
        }
    }
    return err
}

func markerAPP1discriminator( header []byte ) int {
    if bytes.Equal( header[0:6], []byte( "Exif\x00\x00" ) ) {
        return _APP1_EXIF
    }
    if bytes.Equal( header[0:29], []byte( "http://ns.adobe.com/xap/1.0/\x00" ) ) {
        return _APP1_XMP
    }
    return -1
}

func (jpg *Desc) app1( marker, sLen uint ) error {
    if sLen < 8 {
        return fmt.Errorf( "app1: Wrong APP1 (EXIF, TIFF) header (invalid length %d)\n", sLen )
    }
    if jpg.state != _APPLICATION {
        return fmt.Errorf( "app1: Wrong sequence %s in state %s\n",
                           getJPEGmarkerName(_APP1), jpg.getJPEGStateName() )
    }
    offset := jpg.offset + 4    // points 1 byte after length
    appType := markerAPP1discriminator( jpg.data[offset:] )
    var err error
    switch appType {
    case _APP1_EXIF:
        err = jpg.exifApplication( offset, sLen-2 )
    case _APP1_XMP:
        err = jpg.xmpApplication( offset, sLen-2 )
    default:
        err = fmt.Errorf( "app1: Wrong APP1 header (%s)\n", jpg.data[offset:offset+6] )
    }
    return err
}

