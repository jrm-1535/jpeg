package jpeg

// support for JPEG app0 (JFIF)

import (
    "fmt"
    "bytes"
)

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

func ismarkerSOFn( marker uint ) bool {
    if marker < _SOF0 || marker > _SOF15 { return false }
    if marker == _DHT || marker == _JPG || marker == _DAC { return false }
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

