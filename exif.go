package jpeg

// support for JPEG APP1 (EXIF, TIFF)

import (
    "fmt"
    "bytes"
    "strings"
//    "os"
)

// app1 specific support

const (
    _UnsignedByte = 1
    _ASCIIString = 2
    _UnsignedShort = 3
    _UnsignedLong = 4
    _UnsignedRational = 5
    _SignedByte = 6
    _Undefined = 7
    _SignedShort = 8
    _SignedLong = 9
    _SignedRational = 10
    _Float = 11
    _Double = 12
)

const (
    _ByteSize       = 1
    _ShortSize      = 2
    _LongSize       = 4
    _RationalSize   = 8
    _FloatSize      = 4
    _DoubleSize     = 8
)

func (jpg *JpegDesc)getByte( offset uint ) byte {
    return jpg.data[offset]
}

func (jpg *JpegDesc)getBytes( offset, count uint ) []byte {
    vSlice := make( []byte, count )
    for i := uint(0); i < count; i++ {
        vSlice[i] = jpg.getByte( offset )
        offset += _ByteSize
    }
    return vSlice
}

func (jpg *JpegDesc) getBytesFromIFD( lEndian bool,
                                      fCount, fOffset, origin uint ) []byte {
    if fCount <= 4 {
        return jpg.getBytes( fOffset, fCount )
    }
    offset := jpg.getUnsignedLong( lEndian, fOffset )
    return jpg.getBytes( offset + origin, fCount )
}

func (jpg *JpegDesc)getASCIIString( offset, count uint ) string {
    var b strings.Builder
    b.Write( jpg.data[offset:offset+count] )
    return b.String()
}

func (jpg *JpegDesc) getUnsignedShort( littleEndian bool, offset uint ) uint {
    if littleEndian {
        return (uint(jpg.data[offset+1]) << 8) + uint(jpg.data[offset])
    }
    return (uint(jpg.data[offset]) << 8) + uint(jpg.data[offset+1])
}

func (jpg *JpegDesc) getUnsignedShorts( littleEndian bool, offset, count uint ) []uint {
    vSlice := make( []uint, count )
    for i := uint(0); i < count; i++ {
        vSlice[i] = jpg.getUnsignedShort( littleEndian, offset )
        offset += _ShortSize
    }
    return vSlice
}

func (jpg *JpegDesc) getTiffUnsignedShortsFromIFD( lEndian bool,
                                                   fCount, fOffset, origin uint ) []uint {
    if fCount * _ShortSize <= 4 {
        return jpg.getUnsignedShorts( lEndian, fOffset, fCount )
    } else {
        offset := jpg.getUnsignedLong( lEndian, fOffset )
        return jpg.getUnsignedShorts( lEndian, offset + origin, fCount )
    }
}

func (jpg *JpegDesc) getUnsignedLong( littleEndian bool, offset uint ) uint {
    if littleEndian {
        return (uint(jpg.data[offset+3]) << 24) + (uint(jpg.data[offset+2] << 16) +
                uint(jpg.data[offset+1]) << 8) + uint(jpg.data[offset])
    }
    return (uint(jpg.data[offset]) << 24) + (uint(jpg.data[offset+1] << 16) +
            uint(jpg.data[offset+2]) << 8) + uint(jpg.data[offset+3])
}

func (jpg *JpegDesc) getUnsignedLongs( littleEndian bool, offset, count uint ) []uint {
    vSlice := make( []uint, count )
    for i := uint(0); i < count; i++ {
        vSlice[i] = jpg.getUnsignedLong( littleEndian, offset )
        offset += _LongSize
    }
    return vSlice
}

type rational struct {
    numerator, denominator  uint
}

func (jpg *JpegDesc) getUnsignedRational( littleEndian bool, offset uint ) rational {
    var rVal rational
    rVal.numerator = jpg.getUnsignedLong( littleEndian, offset )
    rVal.denominator = jpg.getUnsignedLong( littleEndian, offset + _LongSize )
    return rVal
}

func (jpg *JpegDesc) getUnsignedRationals( littleEndian bool, offset, count uint ) []rational {
    vSlice := make( []rational, count )
    for i := uint(0); i < count; i++ {
        vSlice[i] = jpg.getUnsignedRational( littleEndian, offset )
        offset += _RationalSize
    }
    return vSlice
}

type sRational struct {
    numerator, denominator  int
}

func (jpg *JpegDesc) getSignedRational( littleEndian bool, offset uint ) sRational {
    var srVal sRational
    srVal.numerator = int(jpg.getUnsignedLong( littleEndian, offset ))
    srVal.denominator = int(jpg.getUnsignedLong( littleEndian, offset + _LongSize ))
    return srVal
}

func getTiffTString( tiffT uint ) string {
    switch tiffT {
        case _UnsignedByte: return "byte"
        case _ASCIIString: return "ASCII string"
        case _UnsignedShort: return "Unsigned short"
        case _UnsignedLong: return "Unsigned long"
        case _UnsignedRational: return "Unsigned rational"
        case _SignedByte: return "Signed byte"
        case _SignedShort: return "Signed short"
        case _SignedLong: return "Signed long"
        case _SignedRational: return "Signed rational"
        case _Float: return "Float"
        case _Double: return "Double"
        default: break
    }
    return "Undefined"
}

func (jpg *JpegDesc) checkTiffByte( name string, lEndian bool,
                                    fType, fCount,
                                    fOffset, origin uint,
                                    fmtUB func( v byte) ) error {
    if fType != _UnsignedByte {
        return fmt.Errorf( "%s: invalid type (%s)\n", name, getTiffTString( fType ) )
    }
    if fCount != 1 {
        return fmt.Errorf( "%s: invalid count (%d)\n", name, fCount )
    }
    if jpg.Content {
        value := jpg.getByte( fOffset )
        if fmtUB == nil {
            fmt.Printf( "    %s: %d\n", name, value )
        } else {
            fmt.Printf( "    %s: ", name )
            fmtUB( value )
        }        
    }
    return nil
}

func (jpg *JpegDesc) checkTiffAscii( name string, lEndian bool,
                                     fType, fCount, fOffset, origin uint ) error {
    if fType != _ASCIIString {
        return fmt.Errorf( "%s: invalid type (%s)\n",
                            name, getTiffTString( fType ) )
    }
    if jpg.Content {
        var text string
        if fCount <= 4 {
            text = jpg.getASCIIString( fOffset, fCount )
        } else {
            offset := jpg.getUnsignedLong( lEndian, fOffset )
            text = jpg.getASCIIString( offset + origin, fCount )
        }
        fmt.Printf( "    %s: %s\n", name, text )
    }
    return nil
}

func (jpg *JpegDesc) checkTiffUnsignedShort( name string, lEndian bool,
                                             fType, fCount,
                                             fOffset, origin uint,
                                             fmtUS func( v uint) ) error {
    if fType != _UnsignedShort {
        return fmt.Errorf( "%s: invalid type (%s)\n", name, getTiffTString( fType ) )
    }
    if fCount != 1 {
        return fmt.Errorf( "%s: invalid count (%d)\n", name, fCount )
    }
    if jpg.Content {
        value := jpg.getUnsignedShort( lEndian, fOffset )
        if fmtUS == nil {
            fmt.Printf( "    %s: %d\n", name, value )
        } else {
            fmt.Printf( "    %s: ", name )
            fmtUS( value )
        }        
    }
    return nil
}

func (jpg *JpegDesc) checkTiffUnsignedShorts( name string, lEndian bool,
                                              fType, fCount, fOffset, origin uint ) error {
    if fType != _UnsignedShort {
        return fmt.Errorf( "%s: invalid type (%s)\n",
                            name, getTiffTString( fType ) )
    }
    if jpg.Content {
        values := jpg.getTiffUnsignedShortsFromIFD( lEndian, fCount, fOffset, origin )
        fmt.Printf( "    %s:", name )
        for _, v := range values {
            fmt.Printf( " %d", v )
        }
        fmt.Printf( "\n");
    }
    return nil
}

func (jpg *JpegDesc) checkTiffUnsignedLong( name string, lEndian bool,
                                            fType, fCount,
                                            fOffset, origin uint,
                                            fmtUS func( v uint) ) error {
    if fType != _UnsignedLong {
        return fmt.Errorf( "%s: invalid type (%s)\n", name, getTiffTString( fType ) )
    }
    if fCount != 1 {
        return fmt.Errorf( "%s: invalid count (%d)\n", name, fCount )
    }
    if jpg.Content {
        value := jpg.getUnsignedLong( lEndian, fOffset )
        if fmtUS == nil {
            fmt.Printf( "    %s: %d\n", name, value )
        } else {
            fmt.Printf( "    %s: ", name )
            fmtUS( value )
        }        
    }
    return nil
}

func (jpg *JpegDesc) checkTiffUnsignedRational( name string, lEndian bool,
                                                fType, fCount,
                                                fOffset, origin uint,
                                                fmtUR func( v rational ) ) error {
    if fType != _UnsignedRational {
        return fmt.Errorf( "%s: invalid type (%s)\n",
                            name, getTiffTString( fType ) )
    }
    if fCount != 1 {
        return fmt.Errorf( "%s: invalid count (%d)\n", name, fCount )
    }
    if jpg.Content {
        // a rational never fits directly in valOffset (requires more than 4 bytes)
        offset := jpg.getUnsignedLong( lEndian, fOffset )
        v := jpg.getUnsignedRational( lEndian, offset + origin )
        if fmtUR == nil {
            fmt.Printf( "    %s: %d/%d=%f\n", name, v.numerator, v.denominator,
                        float32(v.numerator)/float32(v.denominator) )
        } else {
            fmt.Printf( "    %s: ", name )
            fmtUR( v )
        }
    }
    return nil
}

func (jpg *JpegDesc) checkTiffSignedRational( name string, lEndian bool,
                                              fType, fCount,
                                               fOffset, origin uint,
                                               fmtSR func( v sRational ) ) error {
    if fType != _SignedRational {
        return fmt.Errorf( "%s: invalid type (%s)\n",
                            name, getTiffTString( fType ) )
    }
    if fCount != 1 {
        return fmt.Errorf( "%s: invalid count (%d)\n", name, fCount )
    }
    if jpg.Content {
        // a rational never fits directly in valOffset (requires more than 4 bytes)
        offset := jpg.getUnsignedLong( lEndian, fOffset )
        v := jpg.getSignedRational( lEndian, offset + origin )
        if fmtSR == nil {
            fmt.Printf( "    %s: %d/%d=%f\n", name, v.numerator, v.denominator,
                        float32(v.numerator)/float32(v.denominator) )
        } else {
            fmt.Printf( "    %s: ", name )
            fmtSR( v )
        }
    }
    return nil
}

func (jpg *JpegDesc) checkTiffUnsignedRationals( name string, lEndian bool,
                                                 fType, fCount, fOffset, origin uint ) error {
    if fType != _UnsignedRational {
        return fmt.Errorf( "%s: invalid type (%s)\n",
                            name, getTiffTString( fType ) )
    }
    if jpg.Content {
        // a rational never fits directly in valOffset (requires 8 bytes)
        offset := jpg.getUnsignedLong( lEndian, fOffset )
        values := jpg.getUnsignedRationals( lEndian, offset + origin, fCount )
        fmt.Printf( "    %s:", name )
        for _, v := range values {
            fmt.Printf( " %d/%d", v.numerator, v.denominator,
                        float32(v.numerator)/float32(v.denominator) )
        }
        fmt.Printf( "\n");
    }
    return nil
}


const (
    _PRIMARY    = 0     // namespace for IFD0, first IFD
    _THUMBNAIL  = 1     // namespace for IFD1 pointed to by IFD0
    _EXIF       = 2     // exif namespace, pointed to by IFD0
    _GPS        = 3     // gps namespace, pointed to by IFD0
    _IOP        = 4     // Interoperability namespace, pointed to by Exif IFD
)

const (                                     // _PRIMARY & _THUMBNAIL IFD tags
//    _NewSubfileType             = 0xfe    // unused in Exif files
//    _SubfileType                = 0xff    // unused in Exif files
    _ImageWidth                 = 0x100
    _ImageLength                = 0x101
    _BitsPerSample              = 0x102
    _Compression                = 0x103

    _PhotometricInterpretation  = 0x106
    _Threshholding              = 0x107
    _CellWidth                  = 0x108
    _CellLength                 = 0x109
    _FillOrder                  = 0x10a

    _DocumentName               = 0x10d
    _ImageDescription           = 0x10e
    _Make                       = 0x10f
    _Model                      = 0x110
    _StripOffsets               = 0x111
    _Orientation                = 0x112

    _SamplesPerPixel            = 0x115
    _RowsPerStrip               = 0x116
    _StripByteCounts            = 0x117
    _MinSampleValue             = 0x118
    _MaxSampleValue             = 0x119
    _XResolution                = 0x11a
    _YResolution                = 0x11b
    _PlanarConfiguration        = 0x11c
    _PageName                   = 0x11d
    _XPosition                  = 0x11e
    _YPosition                  = 0x11f
    _FreeOffsets                = 0x120
    _FreeByteCounts             = 0x121
    _GrayResponseUnit           = 0x122
    _GrayResponseCurve          = 0x123
    _T4Options                  = 0x124
    _T6Options                  = 0x125

    _ResolutionUnit             = 0x128
    _PageNumber                 = 0x129

    _TransferFunction           = 0x12d

    _Software                   = 0x131
    _DateTime                   = 0x132

    _Artist                     = 0x13b
    _HostComputer               = 0x13c
    _Predictor                  = 0x13d
    _WhitePoint                 = 0x13e
    _PrimaryChromaticities      = 0x13f
    _ColorMap                   = 0x140
    _HalftoneHints              = 0x141
    _TileWidth                  = 0x142
    _TileLength                 = 0x143
    _TileOffsets                = 0x144
    _TileByteCounts             = 0x145

    _InkSet                     = 0x14c
    _InkNames                   = 0x14d
    _NumberOfInks               = 0x14e

    _DotRange                   = 0x150
    _TargetPrinter              = 0x151
    _ExtraSamples               = 0x152
    _SampleFormat               = 0x153
    _SMinSampleValue            = 0x154
    _SMaxSampleValue            = 0x155
    _TransferRange              = 0x156

    _JPEGProc                   = 0x200
    _JPEGInterchangeFormat      = 0x201
    _JPEGInterchangeFormatLength = 0x202
    _JPEGRestartInterval        = 0x203

    _JPEGLosslessPredictors     = 0x205
    _JPEGPointTransforms        = 0x206
    _JPEGQTables                = 0x207
    _JPEGDCTables               = 0x208
    _JPEGACTables               = 0x209

    _YCbCrCoefficients          = 0x211
    _YCbCrSubSampling           = 0x212
    _YCbCrPositioning           = 0x213
    _ReferenceBlackWhite        = 0x214

    _Copyright                  = 0x8298

    _ExifIFD                    = 0x8769

    _GpsIFD                     = 0x8825
)

func (jpg *JpegDesc) checkTiffCompression( ifd, fType, fCount, fOffset, origin uint,
                                           lEndian bool ) error {
/*
    Exif2-2: optional in Primary IFD and in thumbnail IFD
When a primary image is JPEG compressed, this designation is not necessary and is omitted.
When thumbnails use JPEG compression, this tag value is set to 6.
*/
    fmtCompression := func( v uint ) {
        var cString string
        switch( v ) {
        case 1: cString = "No compression"
        case 2: cString = "CCITT 1D modified Huffman RLE"
        case 3: cString = "CCITT Group 3 fax encoding"
        case 4: cString = "CCITT Group 4 fax encoding"
        case 5: cString = "LZW"
        case 6: cString = "JPEG"
        case 7: cString = "JPEG (Technote2)"
        case 8: cString = "Deflate"
        case 9: cString = "RFC 2301 (black and white JBIG)."
        case 10: cString = "RFC 2301 (volor JBIG)."
        case 32773: cString = "PackBits compression (Macintosh RLE)"
        default:
            fmt.Printf( "Illegal compression (%d)\n", v )
            return
        }
        fmt.Printf( "%s\n", cString )
        if ifd == _PRIMARY {
            if v != 6 {
                fmt.Printf("    Warning: non-JPEG compression specified in a JPEG file\n" )
            } else {
                fmt.Printf("    Warning: Exif2-2 specifies that in case of JPEG picture compression be omited\n")
            }
        }
    }
    return jpg.checkTiffUnsignedShort( "Compression", lEndian, fType, fCount,
                                        fOffset, origin, fmtCompression )
}

func (jpg *JpegDesc) checkTiffOrientation( ifd, fType, fCount, fOffset, origin uint,
                                           lEndian bool ) error {
    fmtOrientation := func( v uint ) {
        var oString string
        switch( v ) {
        case 1: oString = "Row #0 Top, Col #0 Left"
        case 2: oString = "Row #0 Top, Col #0 Right"
        case 3: oString = "Row #0 Bottom, Col #0 Right"
        case 4: oString = "Row #0 Bottom, Col #0 Left"
        case 5: oString = "Row #0 Left, Col #0 Top"
        case 6: oString = "Row #0 Right, Col #0 Top"
        case 7: oString = "Row #0 Right, Col #0 Bottom"
        case 8: oString = "Row #0 Left, Col #0 Bottom"
        default:
            fmt.Printf( "Illegal orientation (%d)\n", v )
            return
        }
        fmt.Printf( "%s\n", oString )
    }
    return jpg.checkTiffUnsignedShort( "Orientation", lEndian, fType, fCount,
                                        fOffset, origin, fmtOrientation )
}

func (jpg *JpegDesc) checkTiffResolutionUnit( ifd, fType, fCount, fOffset, origin uint,
                                              lEndian bool ) error {
    fmtResolutionUnit := func( v uint ) {
        var ruString string
        switch( v ) {
        case 1 : ruString = "Dots per Arbitrary unit"
        case 2 : ruString = "Dots per Inch"
        case 3 : ruString = "Dots per Cm"
        default:
            fmt.Printf( "Illegal resolution unit (%d)\n", v )
            return
        }
        fmt.Printf( "%s\n", ruString )
    }
    return jpg.checkTiffUnsignedShort( "ResolutionUnit", lEndian, fType, fCount,
                                        fOffset, origin, fmtResolutionUnit )
}

func (jpg *JpegDesc) checkTiffYCbCrPositioning( ifd, fType, fCount, fOffset, origin uint,
                                              lEndian bool ) error {
    fmtYCbCrPositioning := func( v uint ) {
        var posString string
        switch( v ) {
        case 1 : posString = "Centered"
        case 2 : posString = "Cosited"
        default:
            fmt.Printf( "Illegal positioning (%d)\n", v )
            return
        }
        fmt.Printf( "%s\n", posString )
    }
    return jpg.checkTiffUnsignedShort( "YCbCrPositioning", lEndian, fType, fCount,
                                       fOffset, origin, fmtYCbCrPositioning )
}

func (jpg *JpegDesc) checkTiffTag( ifd, tag, fType, fCount, fOffset, origin uint,
                                   lEndian bool ) error {
    switch tag {
    case _Compression:
        return jpg.checkTiffCompression( ifd, fType, fCount, fOffset, origin, lEndian )
    case _Make:
        return jpg.checkTiffAscii( "Make", lEndian, fType, fCount, fOffset, origin )
    case _Model:
        return jpg.checkTiffAscii( "Model", lEndian, fType, fCount, fOffset, origin )
    case _Orientation:
        return jpg.checkTiffOrientation( ifd, fType, fCount, fOffset, origin, lEndian )
    case _XResolution:
        return jpg.checkTiffUnsignedRational( "XResolution", lEndian, fType, fCount,
                                              fOffset, origin, nil )
    case _YResolution:
        return jpg.checkTiffUnsignedRational( "YResolution", lEndian, fType, fCount,
                                              fOffset, origin, nil )
    case _ResolutionUnit:
        return jpg.checkTiffResolutionUnit( ifd, fType, fCount, fOffset, origin, lEndian )
    case _Software:
        return jpg.checkTiffAscii( "Software", lEndian, fType, fCount, fOffset, origin )
    case _DateTime:
        return jpg.checkTiffAscii( "Date", lEndian, fType, fCount, fOffset, origin )
    case _YCbCrPositioning:
        return jpg.checkTiffYCbCrPositioning( ifd, fType, fCount, fOffset, origin, lEndian )
    case _Copyright:
        return jpg.checkTiffAscii( "Copyright", lEndian, fType, fCount, fOffset, origin )
    }
    return fmt.Errorf( "checkTiffTag: unknown or unsupported tag (%#02x) @offset %#04x count %d\n",
                       tag, fOffset, fCount )
}

const (                                     // _EXIF IFD specific tags
    _ExposureTime               = 0x829a

    _FNumber                    = 0x829d

    _ExposureProgram            = 0x8822

    _ISOSpeedRatings            = 0x8827

    _ExifVersion                = 0x9000

    _DateTimeOriginal           = 0x9003
    _DateTimeDigitized          = 0x9004

    _ComponentsConfiguration    = 0x9101
    _CompressedBitsPerPixel     = 0x9102

    _ShutterSpeedValue          = 0x9201
    _ApertureValue              = 0x9202
    _BrightnessValue            = 0x9203
    _ExposureBiasValue          = 0x9204
    _MaxApertureValue           = 0x9205

    _MeteringMode               = 0x9207
    _LightSource                = 0x9208
    _Flash                      = 0x9209
    _FocalLength                = 0x920a

    _SubjectArea                = 0x9214

    _MakerNote                  = 0x927c

    _UserComment                = 0x9286

    _SubsecTime                 = 0x9290
    _SubsecTimeOriginal         = 0x9291
    _SubsecTimeDigitized        = 0x9292

    _FlashpixVersion            = 0xa000
    _ColorSpace                 = 0xa001
    _PixelXDimension            = 0xa002
    _PixelYDimension            = 0xa003

    _InteroperabilityIFD        = 0xa005

    _SubjectLocation            = 0xa214
    _SensingMethod              = 0xa217

    _FileSource                 = 0xa300
    _SceneType                  = 0xa301
    _CFAPattern                 = 0xa302

    _CustomRendered             = 0xa401
    _ExposureMode               = 0xa402
    _WhiteBalance               = 0xa403
    _DigitalZoomRatio           = 0xa404
    _FocalLengthIn35mmFilm      = 0xa405
    _SceneCaptureType           = 0xa406
    _GainControl                = 0xa407
    _Contrast                   = 0xa408
    _Saturation                 = 0xa409
    _Sharpness                  = 0xa40a

    _SubjectDistanceRange       = 0xa40c

    _LensSpecification          = 0xa432
    _LensMake                   = 0xa433
    _LensModel                  = 0xa434
)

func (jpg *JpegDesc) checkExifVersion( fType, fCount, fOffset, origin uint,
                                       lEndian bool ) error {
  // special case: tiff type is undefined, but it is actually ASCII
    if fType != _Undefined {
        return fmt.Errorf( "ExifVersion: invalid byte type (%s)\n", getTiffTString( fType ) )
    }
    return jpg.checkTiffAscii( "ExifVersion", lEndian, _ASCIIString, fCount, fOffset, origin )
}

func (jpg *JpegDesc) checkExifExposureTime( fType, fCount, fOffset, origin uint,
                                            lEndian bool ) error {
    fmtExposureTime := func( v rational ) {
        fmt.Printf( "%f seconds\n", float32(v.numerator)/float32(v.denominator) )
    }
    return jpg.checkTiffUnsignedRational( "ExposureTime",lEndian, fType, fCount,
                                          fOffset, origin, fmtExposureTime )
}

func (jpg *JpegDesc) checkExifExposureProgram( fType, fCount, fOffset, origin uint,
                                               lEndian bool ) error {
    fmtExposureProgram := func( v uint ) {
        var epString string
        switch v {
        case 0 : epString = "Undefined"
        case 1 : epString = "Manual"
        case 2 : epString = "Normal program"
        case 3 : epString = "Aperture priority"
        case 4 : epString = "Shutter priority"
        case 5 : epString = "Creative program (biased toward depth of field)"
        case 6 : epString = "Action program (biased toward fast shutter speed)"
        case 7 : epString = "Portrait mode (for closeup photos with the background out of focus)"
        case 8 : epString = "Landscape mode (for landscape photos with the background in focus) "
        default:
            fmt.Printf( "Illegal Exposure Program (%d)\n", v )
            return
        }
        fmt.Printf( "%s\n", epString )
    }
    return jpg.checkTiffUnsignedShort( "ExposureProgram", lEndian, fType, fCount,
                                        fOffset, origin, fmtExposureProgram )
}

func (jpg *JpegDesc) checkExifComponentsConfiguration( fType, fCount, fOffset, origin uint,
                                                       lEndian bool ) error {
    if fType != _Undefined {  // special case: tiff type is undefined, but it is actually bytes
        return fmt.Errorf( "ComponentsConfiguration: invalid type (%s)\n", getTiffTString( fType ) )
    }
    if fCount != 4 {
        return fmt.Errorf( "ComponentsConfiguration: invalid byte count (%d)\n", fCount )
    }
    bSlice := jpg.getBytes( fOffset, fCount )
    var config strings.Builder
    for _, b := range bSlice {
        switch b {
        case 0:
        case 1: config.WriteByte( 'Y' )
        case 2: config.WriteString( "Cb" )
        case 3: config.WriteString( "Cr" )
        case 4: config.WriteByte( 'R' )
        case 5: config.WriteByte( 'G' )
        case 6: config.WriteByte( 'B' )
        default: config.WriteByte( '?' )
        }
    }
    fmt.Printf( "    ComponentsConfiguration: %s\n", config.String() )
    return nil
}

func (jpg *JpegDesc) checkExifMeteringMode( fType, fCount, fOffset, origin uint,
                                            lEndian bool ) error {
    fmtMeteringMode := func( v uint ) {
        var mmString string
        switch v {
        case 0 : mmString = "Unknown"
        case 1 : mmString = "Average"
        case 2 : mmString = "CenterWeightedAverage program"
        case 3 : mmString = "Spot"
        case 4 : mmString = "MultiSpot"
        case 5 : mmString = "Pattern"
        case 6 : mmString = "Partial"
        case 255: mmString = "Other"
        default:
            fmt.Printf( "Illegal Metering Mode (%d)\n", v )
            return
        }
        fmt.Printf( "%s\n", mmString )
    }
    return jpg.checkTiffUnsignedShort( "MeteringMode", lEndian, fType, fCount,
                                        fOffset, origin, fmtMeteringMode )
}

func (jpg *JpegDesc) checkExifLightSource( fType, fCount, fOffset, origin uint,
                                           lEndian bool ) error {
    fmtLightSource := func( v uint ) {
        var lsString string
        switch v {
        case 0 : lsString = "Unknown"
        case 1 : lsString = "Daylight"
        case 2 : lsString = "Fluorescent"
        case 3 : lsString = "Tungsten (incandescent light)"
        case 4 : lsString = "Flash"
        case 9 : lsString = "Fine weather"
        case 10 : lsString = "Cloudy weather"
        case 11 : lsString = "Shade"
        case 12 : lsString = "Daylight fluorescent (D 5700 - 7100K)"
        case 13 : lsString = "Day white fluorescent (N 4600 - 5400K)"
        case 14 : lsString = "Cool white fluorescent (W 3900 - 4500K)"
        case 15 : lsString = "White fluorescent (WW 3200 - 3700K)"
        case 17 : lsString = "Standard light A"
        case 18 : lsString = "Standard light B"
        case 19 : lsString = "Standard light C"
        case 20 : lsString = "D55"
        case 21 : lsString = "D65"
        case 22 : lsString = "D75"
        case 23 : lsString = "D50"
        case 24 : lsString = "ISO studio tungsten"
        case 255: lsString = "Other light source"
        default:
            fmt.Printf( "Illegal light source (%d)\n", v )
            return
        }
        fmt.Printf( "%s\n", lsString )
    }
    return jpg.checkTiffUnsignedShort( "LightSource", lEndian, fType, fCount,
                                        fOffset, origin, fmtLightSource )
}

func (jpg *JpegDesc) checkExifFlash( fType, fCount, fOffset, origin uint,
                                     lEndian bool ) error {
    fmtFlash := func( v uint ) {
        var fString string
        switch v {
        case 0x00 : fString = "Flash did not fire"
        case 0x01 : fString = "Flash fired"
        case 0x05 : fString = "Flash fired, strobe return light not detected"
        case 0x07 : fString = "Flash fired, strobe return light detected"
        case 0x09 : fString = "Flash fired, compulsory flash mode, return light not detected"
        case 0x0F : fString = "Flash fired, compulsory flash mode, return light detected"
        case 0x10 : fString = "Flash did not fire, compulsory flash mode"
        case 0x18 : fString = "Flash did not fire, auto mode"
        case 0x19 : fString = "Flash fired, auto mode"
        case 0x1D : fString = "Flash fired, auto mode, return light not detected"
        case 0x1F : fString = "Flash fired, auto mode, return light detected"
        case 0x20 : fString = "No flash function"
        case 0x41 : fString = "Flash fired, red-eye reduction mode"
        case 0x45 : fString = "Flash fired, red-eye reduction mode, return light not detected"
        case 0x47 : fString = "Flash fired, red-eye reduction mode, return light detected"
        case 0x49 : fString = "Flash fired, compulsory flash mode, red-eye reduction mode"
        case 0x4D : fString = "Flash fired, compulsory flash mode, red-eye reduction mode, return light not detected"
        case 0x4F : fString = "Flash fired, compulsory flash mode, red-eye reduction mode, return light detected"
        case 0x59 : fString = "Flash fired, auto mode, red-eye reduction mode"
        case 0x5D : fString = "Flash fired, auto mode, return light not detected, red-eye reduction mode"
        case 0x5F : fString = "Flash fired, auto mode, return light detected, red-eye reduction mode"
        default:
            fmt.Printf( "Illegal Flash (%#02x)\n", v )
            return
        }
        fmt.Printf( "%s\n", fString )
    }
    return jpg.checkTiffUnsignedShort( "Flash", lEndian, fType, fCount,
                                        fOffset, origin, fmtFlash )
}

func (jpg *JpegDesc) checkExifSubjectArea( fType, fCount, fOffset, origin uint,
                                           lEndian bool ) error {
    if fCount < 2 && fCount > 4 {
        return fmt.Errorf( "ComponentsConfiguration: invalid count (%d)\n", fCount )
    }
    if jpg.Content {
        loc := jpg.getTiffUnsignedShortsFromIFD( lEndian, fCount, fOffset, origin )
        switch fCount {
        case 2:
            fmt.Printf( "    Subject Area: Point x=%d, y=%d\n", loc[0], loc[1] )
        case 3:
            fmt.Printf( "    Subject Area: Circle center x=%d, y=%d diameter=%d\n",
                        loc[0], loc[1], loc[2] )
        case 4:
            fmt.Printf( "    Subject Area: Rectangle center x=%d, y=%d width=%d height=%d\n",
                        loc[0], loc[1], loc[2], loc[3] )
        }
    }
    return nil
}

func dumpData( header string, data []byte ) {
    fmt.Printf( "    %s:\n", header )
    for i := 0; i < len(data); i += 16 {
        fmt.Printf("      0x" );
        l := 16
        if len(data)-i < 16 {
            l = len(data)-i
        }
        var b strings.Builder
        j := 0
        for ; j < l; j++ {
            if data[i+j] < 0x20 || data[i+j] > 0x7f {
                b.WriteByte( '.' )
            } else {
                b.WriteByte( data[i+j] )
            }
            fmt.Printf( "%02x ", data[i+j] )
        }
        for ; j < 16; j++ {
            fmt.Printf( "   " )
        }
        fmt.Printf( "%s\n", b.String() )
    }
}

func (jpg *JpegDesc) checkExifMakerNote( fType, fCount, fOffset, origin uint,
                                         lEndian bool ) error {
    if fType != _Undefined {
        return fmt.Errorf( "MakerNote: invalid type (%s)\n", getTiffTString( fType ) )
    }
    if fCount < 4 {
        dumpData( "MakerNote", jpg.data[fOffset:fOffset+fCount] )
    } else {
        offset := jpg.getUnsignedLong( lEndian, fOffset ) + origin
        dumpData( "MakerNote", jpg.data[offset:offset+fCount] )
    }
    return nil
}


func (jpg *JpegDesc) checkExifUserComment( fType, fCount, fOffset, origin uint,
                                           lEndian bool ) error {
    if fType != _Undefined {
        return fmt.Errorf( "UserComment: invalid type (%s)\n", getTiffTString( fType ) )
    }
    if fCount < 8 {
        return fmt.Errorf( "UserComment: invalid count (%s)\n", fCount )
    }
    //  first 8 Bytes are the encoding
    offset := jpg.getUnsignedLong( lEndian, fOffset ) + origin
    encoding := jpg.getBytes( offset, 8 )
    switch encoding[0] {
    case 0x41:  // ASCII?
        if bytes.Equal( encoding, []byte{ 'A', 'S', 'C', 'I', 'I', 0, 0, 0 } ) {
            fmt.Printf( "    UserComment: ITU-T T.50 IA5 (ASCII) [%s]\n", 
                        string(jpg.getBytes( offset+8, fCount-8 )) )
            return nil
        }
    case 0x4a: // JIS?
        if bytes.Equal( encoding, []byte{ 'J', 'I', 'S', 0, 0, 0, 0, 0 } ) {
            fmt.Printf( "    UserComment: JIS X208-1990 (JIS):" )
            dumpData( "UserComment", jpg.data[offset+8:offset+fCount] )
            return nil
        }
    case 0x55:  // UNICODE?
        if bytes.Equal( encoding, []byte{ 'U', 'N', 'I', 'C', 'O', 'D', 'E', 0 } ) {
            fmt.Printf( "    UserComment: Unicode Standard:" )
            dumpData( "UserComment", jpg.data[offset+8:offset+fCount] )
            return nil
        }
    case 0x00:  // Undefined
        if bytes.Equal( encoding, []byte{ 0, 0, 0, 0, 0, 0, 0, 0 } ) {
            fmt.Printf( "    UserComment: Undefined encoding:" )
            dumpData( "UserComment", jpg.data[offset+8:offset+fCount] )
            return nil
        }
    }
    return fmt.Errorf( "UserComment: invalid encoding\n" )
}

func (jpg *JpegDesc) checkFlashpixVersion( fType, fCount, fOffset, origin uint,
                                           lEndian bool ) error {
    if fType == _Undefined && fCount == 4 {
        return jpg.checkTiffAscii( "FlashpixVersion", lEndian, _ASCIIString, fCount, fOffset, origin )
    } else if fType != _Undefined {
        return fmt.Errorf( "FlashpixVersion: invalid type (%s)\n", getTiffTString( fType ) )
    }
    return fmt.Errorf( "FlashpixVersion: incorrect count (%d)\n", fCount )
}

func (jpg *JpegDesc) checkExifColorSpace( fType, fCount, fOffset, origin uint,
                                          lEndian bool ) error {
    fmtColorSpace := func( v uint ) {
        var csString string
        switch v {
        case 1 : csString = "sRGB"
        case 65535: csString = "Uncalibrated"
        default:
            fmt.Printf( "Illegal color space (%d)\n", v )
            return
        }
        fmt.Printf( "%s\n", csString )
    }
    return jpg.checkTiffUnsignedShort( "ColorSpace", lEndian, fType, fCount,
                                        fOffset, origin, fmtColorSpace )
}

func (jpg *JpegDesc) checkExifDimension( name string,
                                         fType, fCount, fOffset, origin uint,
                                         lEndian bool ) error {
    if fType == _UnsignedShort {
        return jpg.checkTiffUnsignedShort( name, lEndian, fType, fCount, fOffset, origin, nil )
    } else if fType == _UnsignedLong {
        return jpg.checkTiffUnsignedLong( name, lEndian, fType, fCount, fOffset, origin, nil )
    }
    return fmt.Errorf( "%s: invalid type (%s)\n", name, getTiffTString( fType ) )
}

func (jpg *JpegDesc) checkExifSensingMethod( fType, fCount, fOffset, origin uint,
                                             lEndian bool ) error {
    fmtSensingMethod := func( v uint ) {
        var smString string
        switch v {
        case 1 : smString = "Undefined"
        case 2 : smString = "One-chip color area sensor"
        case 3 : smString = "Two-chip color area sensor"
        case 4 : smString = "Three-chip color area sensor"
        case 5 : smString = "Color sequential area sensor"
        case 7 : smString = "Trilinear sensor"
        case 8 : smString = "Color sequential linear sensor"
        default:
            fmt.Printf( "Illegal sensing method (%d)\n", v )
            return
        }
        fmt.Printf( "%s\n", smString )
    }
    return jpg.checkTiffUnsignedShort( "SensingMethod", lEndian, fType, fCount,
                                        fOffset, origin, fmtSensingMethod )
}


func (jpg *JpegDesc) checkExifFileSource( fType, fCount, fOffset, origin uint,
                                          lEndian bool ) error {
    if fType != _Undefined {
        return fmt.Errorf( "FileSource: invalid type (%s)\n", getTiffTString( fType ) )
    }
    fmtFileSource := func( v byte ) {       // expect byte
        if v != 3 {
            fmt.Printf( "Illegal file source (%d)\n", v )
            return
        }
        fmt.Printf( "Digital Still Camera (DSC)\n" )
    }
    return jpg.checkTiffByte( "FileSource", lEndian, _UnsignedByte, fCount,
                              fOffset, origin, fmtFileSource )
}

func (jpg *JpegDesc) checkExifSceneType( fType, fCount, fOffset, origin uint,
                                         lEndian bool ) error {
    if fType != _Undefined {
        return fmt.Errorf( "SceneType: invalid type (%s)\n", getTiffTString( fType ) )
    }
    fmtScheneType := func( v byte ) {       // expect byte
        var stString string
        switch v {
        case 1 : stString = "Directly photographed"
        default:
            fmt.Printf( "Illegal schene type (%d)\n", v )
            return
        }
        fmt.Printf( "%s\n", stString )
    }
    return jpg.checkTiffByte( "SceneType", lEndian, _UnsignedByte, fCount,
                              fOffset, origin, fmtScheneType )
}

func (jpg *JpegDesc) checkExifCFAPattern( fType, fCount, fOffset, origin uint,
                                          lEndian bool ) error {
    if fType != _Undefined {
        return fmt.Errorf( "CFAPattern: invalid type (%s)\n", getTiffTString( fType ) )
    }
    // structure describing the color filter array (CFA)
    // 2 short words: horizontal repeat pixel unit (h), vertical repeat pixel unit (v)
    // followed by h*v bytes, each byte value indicating a color:
    // 0 RED, 1 GREEN, 2 BLUE, 3 CYAN, 4 MAGENTA, 5 YELLOW, 6 WHITE
    // Since the structure cannot fit in 4 bytes, its location is inficated by an offset
    offset := jpg.getUnsignedLong( lEndian, fOffset ) + origin
    h := jpg.getUnsignedShort( lEndian, offset )
    v := jpg.getUnsignedShort( lEndian, offset + 2 )
    offset += 4
    c := jpg.getBytes( offset, h * v )
    fmt.Printf( "    CFAPattern:" )
    for i := uint(0); i < v; i++ {
        fmt.Printf("\n      Row %d:", i )
        for j := uint(0); j < h; j++ {
            var s string
            switch c[(i*h)+j] {
            case 0: s = "RED"
            case 1: s = "GREEN"
            case 2: s = "BLUE"
            case 3: s = "CYAN"
            case 4: s = "MAGENTA"
            case 5: s = "YELLOW"
            case 6: s = "WHITE"
            default:
                return fmt.Errorf( "\nCFAPattern: invalid color (%d)\n", c[(i*h)+j] )
            }
            fmt.Printf( " %s", s )
        }
    }
    fmt.Printf( "\n" )
    return nil
}

func(jpg *JpegDesc) checkExifCustomRendered( fType, fCount, fOffset, origin uint,
                                             lEndian bool ) error {
    fmtCustomRendered := func( v uint ) {
        var crString string
        switch v {
        case 0 : crString = "Normal process"
        case 1 : crString = "Custom process"
        default:
            fmt.Printf( "Illegal rendering process (%d)\n", v )
            return
        }
        fmt.Printf( "%s\n", crString )
    }
    return jpg.checkTiffUnsignedShort( "CustomRendered", lEndian, fType, fCount,
                                       fOffset, origin, fmtCustomRendered )
}

func(jpg *JpegDesc) checkExifExposureMode( fType, fCount, fOffset, origin uint,
                                           lEndian bool ) error {
    fmtExposureMode := func( v uint ) {
        var emString string
        switch v {
        case 0 : emString = "Auto exposure"
        case 1 : emString = "Manual exposure"
        case 3 : emString = "Auto bracket"
        default:
            fmt.Printf( "Illegal Exposure mode (%d)\n", v )
            return
        }
        fmt.Printf( "%s\n", emString )
    }
    return jpg.checkTiffUnsignedShort( "ExposureMode", lEndian, fType, fCount,
                                       fOffset, origin, fmtExposureMode )
}

func (jpg *JpegDesc) checkExifWhiteBalance( fType, fCount, fOffset, origin uint,
                                            lEndian bool ) error {
    fmtWhiteBalance := func( v uint ) {
        var wbString string
        switch v {
        case 0 : wbString = "Auto white balance"
        case 1 : wbString = "Manual white balance"
        default:
            fmt.Printf( "Illegal white balance (%d)\n", v )
            return
        }
        fmt.Printf( "%s\n", wbString )
    }
    return jpg.checkTiffUnsignedShort( "WhiteBalance", lEndian, fType, fCount,
                                       fOffset, origin, fmtWhiteBalance )
}

func (jpg *JpegDesc) checkExifDigitalZoomRatio( fType, fCount, fOffset, origin uint,
                                                lEndian bool ) error {
    fmDigitalZoomRatio := func( v rational ) {
        if v.numerator == 0 {
            fmt.Printf( "not used\n" )
        } else if v.denominator == 0 {
            fmt.Printf( "invalid ratio denominator (0)\n" )
        } else {
            fmt.Printf( "%f\n", float32(v.numerator)/float32(v.denominator) )
        }
    }
    return jpg.checkTiffUnsignedRational( "DigitalZoomRatio", lEndian, fType, fCount,
                                         fOffset, origin, fmDigitalZoomRatio )
}

func (jpg *JpegDesc) checkExifSceneCaptureType( fType, fCount, fOffset, origin uint,
                                                lEndian bool ) error {
    fmtSceneCaptureType := func( v uint ) {
        var sctString string
        switch v {
        case 0 : sctString = "Standard"
        case 1 : sctString = "Landscape"
        case 2 : sctString = "Portrait"
        case 3 : sctString = "Night scene"
        default:
            fmt.Printf( "Illegal scene capture type (%d)\n", v )
            return
        }
        fmt.Printf( "%s\n", sctString )
    }
    return jpg.checkTiffUnsignedShort( "SceneCaptureType", lEndian, fType, fCount,
                                       fOffset, origin, fmtSceneCaptureType )
}

func (jpg *JpegDesc) checkExifGainControl( fType, fCount, fOffset, origin uint,
                                           lEndian bool ) error {
    fmtGainControl := func( v uint ) {
        var gcString string
        switch v {
        case 0 : gcString = "none"
        case 1 : gcString = "Low gain up"
        case 2 : gcString = "high gain up"
        case 3 : gcString = "low gain down"
        case 4 : gcString = "high gain down"
        default:
            fmt.Printf( "Illegal gain control (%d)\n", v )
            return
        }
        fmt.Printf( "%s\n", gcString )
    }
    return jpg.checkTiffUnsignedShort( "GainControl", lEndian, fType, fCount,
                                       fOffset, origin, fmtGainControl )
}

func (jpg *JpegDesc) checkExifContrast( fType, fCount, fOffset, origin uint,
                                        lEndian bool ) error {
    fmtContrast := func( v uint ) {
        var cString string
        switch v {
        case 0 : cString = "Normal"
        case 1 : cString = "Soft"
        case 2 : cString = "Hard"
        default:
            fmt.Printf( "Illegal contrast (%d)\n", v )
            return
        }
        fmt.Printf( "%s\n", cString )
    }
    return jpg.checkTiffUnsignedShort( "Contrast", lEndian, fType, fCount,
                                       fOffset, origin, fmtContrast )
}

func (jpg *JpegDesc) checkExifSaturation( fType, fCount, fOffset, origin uint,
                                        lEndian bool ) error {
    fmtSaturation := func( v uint ) {
        var sString string
        switch v {
        case 0 : sString = "Normal"
        case 1 : sString = "Low saturation"
        case 2 : sString = "High saturation"
        default:
            fmt.Printf( "Illegal Saturation (%d)\n", v )
            return
        }
        fmt.Printf( "%s\n", sString )
    }
    return jpg.checkTiffUnsignedShort( "Saturation", lEndian, fType, fCount,
                                       fOffset, origin, fmtSaturation )
}

func (jpg *JpegDesc) checkExifSharpness( fType, fCount, fOffset, origin uint,
                                         lEndian bool ) error {
    fmtSharpness := func( v uint ) {
        var sString string
        switch v {
        case 0 : sString = "Normal"
        case 1 : sString = "Soft"
        case 2 : sString = "Hard"
        default:
            fmt.Printf( "Illegal Sharpness (%d)\n", v )
            return
        }
        fmt.Printf( "%s\n", sString )
    }
    return jpg.checkTiffUnsignedShort( "Sharpness", lEndian, fType, fCount,
                                       fOffset, origin, fmtSharpness )
}

func (jpg *JpegDesc) checkExifDistanceRange( fType, fCount, fOffset, origin uint,
                                         lEndian bool ) error {
    fmtSharpness := func( v uint ) {
        var drString string
        switch v {
        case 0 : drString = "Unknown"
        case 1 : drString = "Macro"
        case 2 : drString = "Close View"
        case 3 : drString = "Distant View"
        default:
            fmt.Printf( "Illegal Distance Range (%d)\n", v )
            return
        }
        fmt.Printf( "%s\n", drString )
    }
    return jpg.checkTiffUnsignedShort( "DistanceRange", lEndian, fType, fCount,
                                       fOffset, origin, fmtSharpness )
}

func (jpg*JpegDesc) checkExifLensSpecification( fType, fCount, fOffset, origin uint,
                                                lEndian bool ) error {
// LensSpecification is an array of ordered rational values:
//  minimum focal length
//  maximum focal length
//  minimum F number in minimum focal length
//  maximum F number in maximum focal length
//  which are specification information for the lens that was used in photography.
//  When the minimum F number is unknown, the notation is 0/0.
    if fCount != 4 {
        return fmt.Errorf( "LensSpecification: invalid count (%d)\n", fCount )
    }
    if fType != _UnsignedRational {
        return fmt.Errorf( "LensSpecification: invalid type (%s)\n", getTiffTString( fType ) )
    }

    if jpg.Content {
        fmt.Printf( "    LensSpecification:\n" )
        offset := jpg.getUnsignedLong( lEndian, fOffset ) + origin

        v := jpg.getUnsignedRational( lEndian, offset )
        fmt.Printf( "      minimum focal length: %d/%d=%f\n",
                    v.numerator, v.denominator,
                    float32(v.numerator)/float32(v.denominator) )
        offset += 8
        v = jpg.getUnsignedRational( lEndian, offset )
        fmt.Printf( "      maximum focal length: %d/%d=%f\n",
                    v.numerator, v.denominator,
                    float32(v.numerator)/float32(v.denominator) )
        offset += 8
        v = jpg.getUnsignedRational( lEndian, offset )
        fmt.Printf( "      minimum F number: %d/%d=%f\n",
                    v.numerator, v.denominator,
                    float32(v.numerator)/float32(v.denominator) )
        offset += 8
        v = jpg.getUnsignedRational( lEndian, offset )
        fmt.Printf( "      maximum F number: %d/%d=%f\n",
                    v.numerator, v.denominator,
                    float32(v.numerator)/float32(v.denominator) )
    }
    return nil
}

func (jpg *JpegDesc) checkExifTag( ifd, tag, fType, fCount, fOffset, origin uint,
                                   lEndian bool ) error {
    switch tag {
    case _ExposureTime:
        return jpg.checkExifExposureTime( fType, fCount, fOffset, origin, lEndian )
    case _FNumber:
        return jpg.checkTiffUnsignedRational( "FNumber", lEndian, fType, fCount,
                                               fOffset, origin, nil )
    case _ExposureProgram:
        return jpg.checkExifExposureProgram( fType, fCount, fOffset, origin, lEndian )

    case _ISOSpeedRatings:
        return jpg.checkTiffUnsignedShorts( "ISOSpeedRatings", lEndian, fType, fCount,
                                            fOffset, origin )
    case _ExifVersion:
        return jpg.checkExifVersion( fType, fCount, fOffset, origin, lEndian )

    case _DateTimeOriginal:
        return jpg.checkTiffAscii( "DateTimeOriginal", lEndian, fType, fCount, fOffset, origin )
    case _DateTimeDigitized:
        return jpg.checkTiffAscii( "DateTimeDigitized", lEndian, fType, fCount, fOffset, origin )

    case _ComponentsConfiguration:
        return jpg.checkExifComponentsConfiguration( fType, fCount, fOffset, origin, lEndian )
    case _CompressedBitsPerPixel:
        return jpg.checkTiffUnsignedRational( "CompressedBitsPerPixel", lEndian, fType, fCount,
                                              fOffset, origin, nil )
    case _ShutterSpeedValue:
        return jpg.checkTiffSignedRational( "ShutterSpeedValue", lEndian, fType, fCount,
                                             fOffset, origin, nil )
    case _ApertureValue:
        return jpg.checkTiffUnsignedRational( "ApertureValue", lEndian, fType, fCount,
                                               fOffset, origin, nil )
    case _BrightnessValue:
        return jpg.checkTiffSignedRational( "BrightnessValue", lEndian, fType, fCount,
                                            fOffset, origin, nil )
    case _ExposureBiasValue:
        return jpg.checkTiffSignedRational( "ExposureBiasValue", lEndian, fType, fCount,
                                            fOffset, origin, nil )
    case _MaxApertureValue:
        return jpg.checkTiffUnsignedRational( "MaxApertureValue", lEndian, fType, fCount,
                                              fOffset, origin, nil )
    case _MeteringMode:
        return jpg.checkExifMeteringMode( fType, fCount, fOffset, origin, lEndian )
    case _LightSource:
        return jpg.checkExifLightSource( fType, fCount, fOffset, origin, lEndian )
    case _Flash:
        return jpg.checkExifFlash( fType, fCount, fOffset, origin, lEndian )
    case _FocalLength:
        return jpg.checkTiffUnsignedRational( "FocalLength", lEndian, fType, fCount,
                                               fOffset, origin, nil )
    case _SubjectArea:
        return jpg.checkExifSubjectArea( fType, fCount, fOffset, origin, lEndian )

    case _MakerNote:
        return jpg.checkExifMakerNote( fType, fCount, fOffset, origin, lEndian )
    case _UserComment:
        return jpg.checkExifUserComment( fType, fCount, fOffset, origin, lEndian )
    case _SubsecTime:
        return jpg.checkTiffAscii( "SubsecTime", lEndian, fType, fCount, fOffset, origin )
    case _SubsecTimeOriginal:
        return jpg.checkTiffAscii( "SubsecTimeOriginal", lEndian, fType, fCount, fOffset, origin )
    case _SubsecTimeDigitized:
        return jpg.checkTiffAscii( "SubsecTimeDigitized", lEndian, fType, fCount, fOffset, origin )
    case _FlashpixVersion:
        return jpg.checkFlashpixVersion( fType, fCount, fOffset, origin, lEndian )

    case _ColorSpace:
        return jpg.checkExifColorSpace( fType, fCount, fOffset, origin, lEndian )
    case _PixelXDimension:
        return jpg.checkExifDimension( "PixelXDimension", fType, fCount, fOffset, origin, lEndian )
    case _PixelYDimension:
        return jpg.checkExifDimension( "PixelYDimension", fType, fCount, fOffset, origin, lEndian )

    case _SensingMethod:
        return jpg.checkExifSensingMethod( fType, fCount, fOffset, origin, lEndian )
    case _FileSource:
        return jpg.checkExifFileSource( fType, fCount, fOffset, origin, lEndian )
    case _SceneType:
        return jpg.checkExifSceneType( fType, fCount, fOffset, origin, lEndian )
    case _CFAPattern:
        return jpg.checkExifCFAPattern( fType, fCount, fOffset, origin, lEndian )
    case _CustomRendered:
        return jpg.checkExifCustomRendered( fType, fCount, fOffset, origin, lEndian )
    case _ExposureMode:
        return jpg.checkExifExposureMode( fType, fCount, fOffset, origin, lEndian )
    case _WhiteBalance:
        return jpg.checkExifWhiteBalance( fType, fCount, fOffset, origin, lEndian )
    case _DigitalZoomRatio:
        return jpg.checkExifDigitalZoomRatio( fType, fCount, fOffset, origin, lEndian )
    case _FocalLengthIn35mmFilm:
        return jpg.checkTiffUnsignedShort( "FocalLengthIn35mmFilm", lEndian, fType, fCount,
                                           fOffset, origin, nil )
    case _SceneCaptureType:
        return jpg.checkExifSceneCaptureType( fType, fCount, fOffset, origin, lEndian )
    case _GainControl:
        return jpg.checkExifGainControl( fType, fCount, fOffset, origin, lEndian )
    case _Contrast:
        return jpg.checkExifContrast( fType, fCount, fOffset, origin, lEndian )
    case _Saturation:
        return jpg.checkExifSaturation( fType, fCount, fOffset, origin, lEndian )
    case _Sharpness:
        return jpg.checkExifSharpness( fType, fCount, fOffset, origin, lEndian )
    case _SubjectDistanceRange:
        return jpg.checkExifDistanceRange( fType, fCount, fOffset, origin, lEndian )
    case _LensSpecification:
        return jpg.checkExifLensSpecification( fType, fCount, fOffset, origin, lEndian )
    case _LensMake:
        return jpg.checkTiffAscii( "LensMake", lEndian, fType, fCount, fOffset, origin )
    case _LensModel:
        return jpg.checkTiffAscii( "LensModel", lEndian, fType, fCount, fOffset, origin )
    }
    return fmt.Errorf( "checkExifTag: unknown or unsupported tag (%#02x) @offset %#04x count %d\n",
                       tag, fOffset, fCount )
}

const (                                     // _GPS IFD specific tags
    _GPSVersionID           = 0x00
    _GPSLatitudeRef         = 0x01
    _GPSLatitude            = 0x02
    _GPSLongitudeRef        = 0x03
    _GPSLongitude           = 0x04
    _GPSAltitudeRef         = 0x05
    _GPSAltitude            = 0x06
    _GPSTimeStamp           = 0x07
    _GPSSatellites          = 0x08
    _GPSStatus              = 0x09
    _GPSMeasureMode         = 0x0a
    _GPSDOP                 = 0x0b
    _GPSSpeedRef            = 0x0c
    _GPSSpeed               = 0x0d
    _GPSTrackRef            = 0x0e
    _GPSTrack               = 0x0f
    _GPSImgDirectionRef     = 0x10
    _GPSImgDirection        = 0x11
    _GPSMapDatum            = 0x12
    _GPSDestLatitudeRef     = 0x13
    _GPSDestLatitude        = 0x14
    _GPSDestLongitudeRef    = 0x15
    _GPSDestLongitude       = 0x16
    _GPSDestBearingRef      = 0x17
    _GPSDestBearing         = 0x18
    _GPSDestDistanceRef     = 0x19
    _GPSDestDistance        = 0x1a
    _GPSProcessingMethod    = 0x1b
    _GPSAreaInformation     = 0x1c
    _GPSDateStamp           = 0x1d
    _GPSDifferential        = 0x1e
)

func (jpg *JpegDesc) checkGPSVersionID( fType, fCount, fOffset, origin uint,
                                        lEndian bool ) error {
    if fCount != 4 {
        return fmt.Errorf( "GPSVersionID: invalid count (%d)\n", fCount )
    }
    if fType != _UnsignedByte {
        return fmt.Errorf( "GPSVersionID: invalid type (%s)\n", getTiffTString( fType ) )
    }
    slc := jpg.getBytes( fOffset, fCount )  // 4 bytes fit in directory entry
    fmt.Printf("    GPSVersionID: %d.%d.%d.%d\n", slc[0], slc[1], slc[2], slc[3] )
    return nil
}

func (jpg *JpegDesc) checkGpsTag( ifd, tag, fType, fCount, fOffset, origin uint,
                                  lEndian bool ) error {
    switch tag {
    case _GPSVersionID:
        return jpg.checkGPSVersionID( fType, fCount, fOffset, origin, lEndian )
    }
    return fmt.Errorf( "checkGpsTag: unknown or unsupported tag (%#02x) @offset %#04x count %d\n",
                       tag, fOffset, fCount )
}

const (                                     // _IOP IFD tags
    _InteroperabilityIndex      = 0x01
    _InteroperabilityVersion    = 0x02
)

func (jpg *JpegDesc) checkInteroperabilityVersion( fType, fCount, fOffset, origin uint,
                                                   lEndian bool ) error {
    if fType != _Undefined {
        return fmt.Errorf( "InteroperabilityVersion: invalid type (%s)\n", getTiffTString( fType ) )
    }
    // assume bytes
    bs := jpg.getBytesFromIFD( lEndian, fCount, fOffset, origin )
    fmt.Printf( "    InteroperabilityVersion: %#02x, %#02x, %#02x, %#02x\n",
                bs[0], bs[1], bs[2], bs[3] )
    return nil
}

func (jpg *JpegDesc) checkIopTag( ifd, tag, fType, fCount, fOffset, origin uint,
                                  lEndian bool ) error {
    switch tag {
    case _InteroperabilityIndex:
        return jpg.checkTiffAscii( "Interoperability", lEndian, fType, fCount, fOffset, origin )
    case _InteroperabilityVersion:
        return jpg.checkInteroperabilityVersion( fType, fCount, fOffset, origin, lEndian )
    default:
        fmt.Printf( "    unknown or unsupported tag (%#02x) @offset %#04x type %s count %d\n",
                    tag, fOffset, getTiffTString( fType), fCount )
    }
//    return fmt.Errorf( "checkIopTag: unknown or unsupported tag (%#02x) @offset %#04x count %d\n",
//                       tag, fOffset, fCount )
    return nil
}

var IfdNames [5]string = [...]string{ "Primary Image data", "Thumbnail Image data",
                                      "Exif data", "GPS data", "Interoperability data" }

func (jpg *JpegDesc) checkIFD( Ifd, IfdOffset, origin uint, tag1, tag2 int,
                               lEndian bool ) ( offset0, offset1, offset2 uint, err error) {

    IfdOffset += origin
    offset1 = 0
    offset2 = 0
    err = nil

    var checkTags func( Ifd, tag, fType, fCount, fOffset, origin uint, lEndian bool ) error
    switch Ifd {
    case _PRIMARY, _THUMBNAIL:  checkTags = jpg.checkTiffTag
    case _EXIF:                 checkTags = jpg.checkExifTag
    case _GPS:                  checkTags = jpg.checkGpsTag
    case _IOP:                  checkTags = jpg.checkIopTag
    }
    /*
        Image File Directory starts with the number of following directory entries (2 bytes)
        followed by that number of entries (12 bytes) and one extra offset to the next IFD
        (4 bytes)
    */
    nIfdEntries := jpg.getUnsignedShort( lEndian, IfdOffset )
    if jpg.Content {
        fmt.Printf( "  IFD #%d %s @%#04x #entries %d\n", Ifd,
                    IfdNames[Ifd], IfdOffset, nIfdEntries )
//        fmt.Printf( "  %s:\n", IfdNames[Ifd] )
    }

    IfdOffset += 2
    for i := uint(0); i < nIfdEntries; i++ {
        tiffTag := jpg.getUnsignedShort( lEndian, IfdOffset )
        tiffType := jpg.getUnsignedShort( lEndian, IfdOffset + 2 )
        tiffCount := jpg.getUnsignedLong( lEndian, IfdOffset + 4 )

        if tag1 != -1 && tiffTag == uint(tag1) {
            offset1 = jpg.getUnsignedLong( lEndian, IfdOffset + 8 )
        } else if tag2 != -1 && tiffTag == uint(tag2) {
            offset2 = jpg.getUnsignedLong( lEndian, IfdOffset + 8 )
        } else {
            err := checkTags( Ifd, tiffTag, tiffType, tiffCount,
                              IfdOffset + 8, origin, lEndian )
            if err != nil {
                return 0, 0, 0, fmt.Errorf( "checkIFD: invalid field: %v\n", err )
            }
        }
        IfdOffset += 12
    }
    offset0 = jpg.getUnsignedLong( lEndian, IfdOffset )
    return
}

func (jpg *JpegDesc) exifApplication( sLen uint ) error {
    if jpg.Content {
        fmt.Printf( "APP1 (EXIF)\n" )
    }
    // Exif\0\0 is followed by TIFF header
    origin := jpg.offset + 10   // TIFF header starts after exif header
    // TIFF header starts with 2 bytes indicating the byte ordering (little or big endian)
    var lEndian bool
    if bytes.Equal( jpg.data[origin:origin+2], []byte( "II" ) ) {
        lEndian = true
    } else if ! bytes.Equal( jpg.data[origin:origin+2], []byte( "MM" ) ) {
        return fmt.Errorf( "exif: invalid TIFF header (unknown byte ordering: %s)\n", jpg.data[origin:origin+2] )
    }

    validTiff := jpg.getUnsignedShort( lEndian, origin+2 )
    if validTiff != 0x2a {
        return fmt.Errorf( "exif: invalid TIFF header (invalid identifier: %d)\n", validTiff )
    }

    // first IFD is the primary image file directory 0
    IFDOffset := jpg.getUnsignedLong( lEndian, origin+4 )
    IFDOffset, exifIFDOffset, gpsIFDOffset, err :=
        jpg.checkIFD( _PRIMARY, IFDOffset, origin, _ExifIFD, _GpsIFD, lEndian )
    if err != nil { return err }
//    fmt.Printf( "IFDOffset %#04x, exifIFDOffset %#04x, gpsIFDOffset %#04x\n",
//                IFDOffset, exifIFDOffset, gpsIFDOffset )

    if IFDOffset != 0 {
        _, thbOffset, thbLength, err := jpg.checkIFD( _THUMBNAIL, IFDOffset, origin,
                                                      _JPEGInterchangeFormat,
                                                      _JPEGInterchangeFormatLength, lEndian )
        if err != nil { return err }

        // decode thumbnail if in JPEG
        fmt.Printf( "============= Thumbnail JPEG picture ================\n" )
        thbOffset += origin
        _, tnErr := Analyze( jpg.data[thbOffset:thbOffset+thbLength],
                             &Control{ Markers: true, Content: true } )
        fmt.Printf( "======================================================\n" )
        if tnErr != nil { return err }
        // save thumnail
        /*
	    f, ferr := os.OpenFile("thbnail", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.ModePerm)
        if ferr != nil { return jpgForwardError( "Write", err ) }
        _, ferr = f.Write( jpg.data[thbOffset:thbOffset+thbLength] )
        if ferr = f.Close( ); ferr != nil { return jpgForwardError( "Write", err ) }
        */
    }

    var ioIFDopOffset uint
    if exifIFDOffset != 0 {
        _, ioIFDopOffset, _, err = jpg.checkIFD( _EXIF, exifIFDOffset, origin, _InteroperabilityIFD, -1, lEndian )
        if err != nil { return err }
    }

    if ioIFDopOffset != 0 {
        _, _, _, err = jpg.checkIFD( _IOP, ioIFDopOffset, origin, -1, -1, lEndian )
        if err != nil { return err }
    }

    if gpsIFDOffset != 0 {
        _, _, _, err = jpg.checkIFD( _GPS, gpsIFDOffset, origin, -1, -1, lEndian )
        if err != nil { return err }
    }
    return nil
}

const (
    _APP1_EXIF = iota
)

func markerAPP1discriminator( h6 []byte ) int {
    if bytes.Equal( h6, []byte( "Exif\x00\x00" ) ) { return _APP1_EXIF }
    // TODO: add other types of APP1
    return -1
}

func (jpg *JpegDesc) app1( marker, sLen uint ) error {
    if sLen < 8 {
        return fmt.Errorf( "app0: Wrong APP1 (EXIF, TIFF) header (invalid length %d)\n", sLen )
    }
    if jpg.state != _APPLICATION {
        return fmt.Errorf( "app0: Wrong sequence %s in state %s\n",
                           getJPEGmarkerName(_APP0), jpg.getJPEGStateName() )
    }
    offset := jpg.offset + 4    // points 1 byte after length
    appType := markerAPP1discriminator( jpg.data[offset:offset+6] )
    if appType == -1 {
        return fmt.Errorf( "app1: Wrong APP1 header (%s)\n", jpg.data[offset:offset+4] )
    }

    return jpg.exifApplication( sLen )
}

