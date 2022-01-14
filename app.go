package jpeg

// support for JPEG app0 (JFIF)

import (
    "fmt"
    "bytes"
    "github.com/jrm-1535/exif"
    "io"
)

// appMetadata interface for all apps
type appMetadata interface {
    serialize( w io.Writer ) (int, error)
    format( w io.Writer ) error
    remove( id int, tag int ) error
    parseThumbnails( ) error
}

type appId  uint
const (
    JFIF appId = iota
    EXIF
)

type app struct {
    id              appId
    data            []appMetadata
}

func (jpg *JpegDesc) storeApp( id appId, data appMetadata ) {
    for _, app := range jpg.apps {
        if app.id == id {
            app.data = append( app.data, data )
            return
        }
    }
    jpg.apps = append( jpg.apps, app{ id, []appMetadata{ data } } )
}

func (jpg *JpegDesc) writeApps( w io.Writer ) (written int, err error) {

    defer func() { if err != nil {err = fmt.Errorf( "writeApps: %v", err )} }()

    var n int
    for _, app := range jpg.apps {
        for _, md := range( app.data ) {
            n, err = md.serialize( w )
            if err != nil {
                return
            }
            written += n
        }
    }
    return
}


// app0 support
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

func (jpg *JpegDesc) app0( marker, sLen uint ) error {
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

    if jpg.Content {
        fmt.Printf( "APP0\n" )
    }
    var err error
    if appType == _APP0_JFIF {
        if len(jpg.tables) != 0 {
            return fmt.Errorf( "app0: APP0 (JFIF) is not the first segment\n" )
        }
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

        err = jpg.addTable( marker, jpg.offset, jpg.offset + 2 + sLen, original )

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
        err = jpg.addTable( marker, jpg.offset, jpg.offset + 2 + sLen, original )
    }
    if err != nil { return jpgForwardError( "app0", err ) }
    return nil
}

// app1 support (Exif, XMP)

const (
    _APP1_EXIF = iota
    _APP1_XMP
)

func (jpg *JpegDesc) xmpApplication( offset, sLen uint ) error {
    if jpg.Content {
        fmt.Printf( "APP1 (XMP)\n" )
        fmt.Printf( "  ----------------- Length %d -----------------\n", sLen )
// TODO: format XML
        fmt.Printf( "%s\n", string(jpg.data[jpg.offset+33:jpg.offset+4+sLen]) )
        fmt.Printf( "  --------------------------------------------------\n" )
    }
    return nil
}

type exifData struct {
    desc *exif.Desc
}

func (ed *exifData) serialize( w io.Writer) (int, error) {
    return ed.desc.Serialize( w )
}

func (ed *exifData) format( w io.Writer) error {
    return ed.desc.Format( w )
}

func (ed *exifData) remove( id int, tag int ) error {
    return ed.desc.Remove( exif.IfdId(id), tag )
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
            _, err = Analyze( data, &Control{ Markers: true, Content: true } )
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

func (jpg *JpegDesc) exifApplication( offset, sLen uint ) error {
    ec := exif.Control{ Unknown: exif.KeepTag, Warn: true }
    d, err := exif.Parse( jpg.data, offset, sLen, &ec )

    if err == nil {
        ed := new( exifData )
        ed.desc = d
        jpg.storeApp( EXIF, ed )

        if jpg.Content {

            if err = ed.parseThumbnails(); err != nil {
                return fmt.Errorf( "exifApplication: %v", err )
            }
            if err = d.Format( nil ); err != nil {
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

func (jpg *JpegDesc) app1( marker, sLen uint ) error {
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

    if err == nil {
        return nil
//        return jpg.addTable( marker, jpg.offset, jpg.offset + 2 + sLen, original )
    }
    return err
}

