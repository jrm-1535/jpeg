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
//    _NewSubfileType             = 0xfe
//    _SubfileType                = 0xff
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

    // Exif IFD specific tags
    _ExposureTime               = 0x829a

    _FNumber                    = 0x829d

    _ExposureProgram            = 0x8822

    _ISOSpeedRatings            = 0x8827

    _ExifVersion                = 0x9000

    _DateTimeOriginal           = 0x9003
    _DateTimeDigitized          = 0x9004
    _ComponentsConfiguration    = 0x9101

    _ShutterSpeedValue          = 0x9201
    _ApertureValue              = 0x9202
    _BrightnessValue            = 0x9203
    _ExposureBiasValue          = 0x9204

    _MeteringMode               = 0x9207

    _Flash                      = 0x9209
    _FocalLength                = 0x920a

    _SubjectArea                = 0x9214

    _MakerNote                  = 0x927c

    _SubsecTimeOriginal         = 0x9291
    _SubsecTimeDigitized        = 0x9292

    _FlashpixVersion            = 0xa000
    _ColorSpace                 = 0xa001
    _PixelXDimension            = 0xa002
    _PixelYDimension            = 0xa003

    _Interoperability           = 0x0a05

    _SubjectLocation            = 0xa214
    _SensingMethod              = 0xa217

    _SceneType                  = 0xa301

    _ExposureMode               = 0xa402
    _WhiteBalance               = 0xa403
    _DigitalZoomRatio           = 0xa404
    _FocalLengthIn35mmFilm      = 0xa405
    _SceneCaptureType           = 0xa406

    _LensSpecification          = 0xa432
    _LensMake                   = 0xa433
    _LensModel                  = 0xa434
)

const (
    _PRIMARY    = 0     // namespace for IFD0, first IFD
    _THUMBNAIL  = 1     // namespace for IFD1 pointed to by IFD0
    _EXIF       = 2     // exif namespace, pointed to by IFD0
    _GPS        = 3     // gps namespace, pointed to by IFD0
)

func (jpg *JpegDesc) checkTiffPrimaryThumbnailAscii( name string,
                                                     ifd, fType, fCount, fOffset, origin uint,
                                                     lEndian bool ) error {
    if ifd != _PRIMARY && ifd != _THUMBNAIL {
        return fmt.Errorf( "%s: tag used outside Primary or thumbnail IFD\n", name )
    }

     return jpg.checkTiffAscii( name, lEndian, fType, fCount, fOffset, origin )
}

func (jpg *JpegDesc) checkTiffCompression( ifd, fType, fCount, fOffset, origin uint,
                                           lEndian bool ) error {
/*
    Exif2-2: optional in Primary IFD and in thumbnail IFD
When a primary image is JPEG compressed, this designation is not necessary and is omitted.
When thumbnails use JPEG compression, this tag value is set to 6.
*/
    if ifd != _PRIMARY && ifd != _THUMBNAIL {
        return fmt.Errorf( "Compression: tag used outside Primary or thumbnail IFD\n" )
    }
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
    if ifd != _PRIMARY && ifd != _THUMBNAIL {
        return fmt.Errorf( "Orientation: tag used outside Primary or thumbnail IFD\n" )
    }
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

func (jpg *JpegDesc) checkTiffResolution( name string,
                                          ifd, fType, fCount, fOffset, origin uint,
                                          lEndian bool ) error {
    if ifd != _PRIMARY && ifd != _THUMBNAIL {
        return fmt.Errorf( "%s: tag used outside Primary or thumbnail IFD\n", name )
    }
    return jpg.checkTiffUnsignedRational( "YResolution", lEndian, fType, fCount,
                                          fOffset, origin, nil )
}

func (jpg *JpegDesc) checkTiffResolutionUnit( ifd, fType, fCount, fOffset, origin uint,
                                              lEndian bool ) error {
    if ifd != _PRIMARY && ifd != _THUMBNAIL {
        return fmt.Errorf( "ResolutionUnit: tag used outside Primary or thumbnail IFD\n" )
    }
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
    if ifd != _PRIMARY && ifd != _THUMBNAIL {
        return fmt.Errorf( "YCbCrPositioning: tag used outside Primary or thumbnail IFD\n" )
    }
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

func (jpg *JpegDesc) checkExifVersion( ifd, fType, fCount, fOffset, origin uint,
                                       lEndian bool ) error {
    if ifd != _EXIF {
        return fmt.Errorf( "ExifVersion: tag used outside EXIF IFD\n" )
    }
  // special case: tiff type is undefined, but it is actually ASCII
    if fType != _Undefined {
        return fmt.Errorf( "ExifVersion: invalid byte type (%s)\n", getTiffTString( fType ) )
    }
    return jpg.checkTiffAscii( "ExifVersion", lEndian, _ASCIIString, fCount, fOffset, origin )
}

func (jpg *JpegDesc) checkExifAscii( name string, ifd, fType, fCount, fOffset, origin uint,
                                     lEndian bool ) error {
    if ifd != _EXIF {
        return fmt.Errorf( "%d: tag used outside EXIF IFD\n", name )
    }
    return jpg.checkTiffAscii( name, lEndian, fType, fCount, fOffset, origin )
}

func (jpg *JpegDesc) checkExifExposureTime( ifd, fType, fCount, fOffset, origin uint,
                                            lEndian bool ) error {
    if ifd != _EXIF {
        return fmt.Errorf( "ExposureTime: tag used outside EXIF IFD\n" )
    }
    fmtExposureTime := func( v rational ) {
        fmt.Printf( "%f seconds\n", float32(v.numerator)/float32(v.denominator) )
    }
    return jpg.checkTiffUnsignedRational( "ExposureTime",lEndian, fType, fCount,
                                          fOffset, origin, fmtExposureTime )
}

func (jpg *JpegDesc) checkExifFNumber( ifd, fType, fCount, fOffset, origin uint,
                                       lEndian bool ) error {
    if ifd != _EXIF {
        return fmt.Errorf( "ExposureTime: tag used outside EXIF IFD\n" )
    }
    return jpg.checkTiffUnsignedRational( "FNumber", lEndian, fType, fCount,
                                          fOffset, origin, nil )
}

func (jpg *JpegDesc) checkExifExposureProgram( ifd, fType, fCount, fOffset, origin uint,
                                               lEndian bool ) error {
    if ifd != _EXIF {
        return fmt.Errorf( "ExposureProgram: tag used outside EXIF IFD\n" )
    }
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

func (jpg *JpegDesc) checkExifISOSpeedRatings( ifd, fType, fCount, fOffset, origin uint,
                                               lEndian bool ) error {
    if ifd != _EXIF {
        return fmt.Errorf( "ISOSpeedRatings: tag used outside EXIF IFD\n" )
    }
    return jpg.checkTiffUnsignedShorts( "ISOSpeedRatings", lEndian, fType, fCount,
                                        fOffset, origin )
}

func (jpg *JpegDesc) checkExifComponentsConfiguration( ifd, fType, fCount, fOffset, origin uint,
                                                       lEndian bool ) error {
    if ifd != _EXIF {
        return fmt.Errorf( "ComponentsConfiguration: tag used outside EXIF IFD\n" )
    }
    if fType != _Undefined {
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

func (jpg *JpegDesc) checkExifShutterSpeedValue( ifd, fType, fCount, fOffset, origin uint,
                                                 lEndian bool ) error {
    if ifd != _EXIF {
        return fmt.Errorf( "ShutterSpeedValue: tag used outside EXIF IFD\n" )
    }
    return jpg.checkTiffSignedRational( "ShutterSpeedValue", lEndian, fType, fCount,
                                         fOffset, origin, nil )
}

func (jpg *JpegDesc) checkExifApertureValue( ifd, fType, fCount, fOffset, origin uint,
                                             lEndian bool ) error {
    if ifd != _EXIF {
        return fmt.Errorf( "ApertureValue: tag used outside EXIF IFD\n" )
    }
    return jpg.checkTiffUnsignedRational( "ApertureValue", lEndian, fType, fCount,
                                           fOffset, origin, nil )
}

func (jpg *JpegDesc) checkExifBrightnessValue( ifd, fType, fCount, fOffset, origin uint,
                                               lEndian bool ) error {
    if ifd != _EXIF {
        return fmt.Errorf( "BrightnessValue: tag used outside EXIF IFD\n" )
    }
    return jpg.checkTiffSignedRational( "BrightnessValue", lEndian, fType, fCount,
                                        fOffset, origin, nil )
}

func (jpg *JpegDesc) checkExifExposureBiasValue( ifd, fType, fCount, fOffset, origin uint,
                                                 lEndian bool ) error {
    if ifd != _EXIF {
        return fmt.Errorf( "ExposureBiasValue: tag used outside EXIF IFD\n" )
    }
    return jpg.checkTiffSignedRational( "ExposureBiasValue", lEndian, fType, fCount,
                                        fOffset, origin, nil )
}

func (jpg *JpegDesc) checkExifMeteringMode( ifd, fType, fCount, fOffset, origin uint,
                                            lEndian bool ) error {
    if ifd != _EXIF {
        return fmt.Errorf( "MeteringMode: tag used outside EXIF IFD\n" )
    }
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

func (jpg *JpegDesc) checkExifFlash( ifd, fType, fCount, fOffset, origin uint,
                                     lEndian bool ) error {
    if ifd != _EXIF {
        return fmt.Errorf( "Flash: tag used outside EXIF IFD\n" )
    }
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

func (jpg *JpegDesc) checkExifFocalLength( ifd, fType, fCount, fOffset, origin uint,
                                           lEndian bool ) error {
    if ifd != _EXIF {
        return fmt.Errorf( "FocalLength: tag used outside EXIF IFD\n" )
    }
    return jpg.checkTiffUnsignedRational( "FocalLength", lEndian, fType, fCount,
                                           fOffset, origin, nil )
}

func (jpg *JpegDesc) checkExifSubjectArea( ifd, fType, fCount, fOffset, origin uint,
                                           lEndian bool ) error {
    if ifd != _EXIF {
        return fmt.Errorf( "SubjectArea: tag used outside EXIF IFD\n" )
    }
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

func (jpg *JpegDesc) checkExifMakerNote( ifd, fType, fCount, fOffset, origin uint,
                                         lEndian bool ) error {
    if ifd != _EXIF {
        return fmt.Errorf( "MakerNote: tag used outside EXIF IFD\n" )
    }
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

func (jpg *JpegDesc) checkFlashpixVersion( ifd, fType, fCount, fOffset, origin uint,
                                           lEndian bool ) error {
    if ifd != _EXIF {
        return fmt.Errorf( "FlashpixVersion: tag used outside EXIF IFD\n" )
    }
    if fType == _Undefined && fCount == 4 {
        return jpg.checkTiffAscii( "FlashpixVersion", lEndian, _ASCIIString, fCount, fOffset, origin )
    } else if fType != _Undefined {
        return fmt.Errorf( "FlashpixVersion: invalid type (%s)\n", getTiffTString( fType ) )
    }
    return fmt.Errorf( "FlashpixVersion: incorrect count (%d)\n", fCount )
}

func (jpg *JpegDesc) checkExifColorSpace( ifd, fType, fCount, fOffset, origin uint,
                                          lEndian bool ) error {
    if ifd != _EXIF {
        return fmt.Errorf( "ColorSpace: tag used outside EXIF IFD\n" )
    }
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
                                         ifd, fType, fCount, fOffset, origin uint,
                                         lEndian bool ) error {
    if ifd != _EXIF {
        return fmt.Errorf( "%s: tag used outside EXIF IFD\n", name )
    }
    if fType == _UnsignedShort {
        return jpg.checkTiffUnsignedShort( name, lEndian, fType, fCount, fOffset, origin, nil )
    } else if fType == _UnsignedLong {
        return jpg.checkTiffUnsignedLong( name, lEndian, fType, fCount, fOffset, origin, nil )
    }
    return fmt.Errorf( "%s: invalid type (%s)\n", name, getTiffTString( fType ) )
}

func (jpg *JpegDesc) checkExifSensingMethod( ifd, fType, fCount, fOffset, origin uint,
                                             lEndian bool ) error {
    if ifd != _EXIF {
        return fmt.Errorf( "SensingMethod: tag used outside EXIF IFD\n" )
    }
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

func (jpg *JpegDesc) checkExifSceneType( ifd, fType, fCount, fOffset, origin uint,
                                         lEndian bool ) error {
    if ifd != _EXIF {
        return fmt.Errorf( "SceneType: tag used outside EXIF IFD\n" )
    }
    if fType != _Undefined {
        return fmt.Errorf( "SceneType: invalid type (%s)\n", getTiffTString( fType ) )
    }
    // expect byte
    fmtScheneType := func( v byte ) {
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

func(jpg *JpegDesc) checkExifExposureMode( ifd, fType, fCount, fOffset, origin uint,
                                           lEndian bool ) error {
    if ifd != _EXIF {
        return fmt.Errorf( "ExposureMode: tag used outside EXIF IFD\n" )
    }
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

func (jpg *JpegDesc) checkExifWhiteBalance( ifd, fType, fCount, fOffset, origin uint,
                                            lEndian bool ) error {
    if ifd != _EXIF {
        return fmt.Errorf( "WhiteBalance: tag used outside EXIF IFD\n" )
    }
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

func (jpg *JpegDesc) checkExifDigitalZoomRatio( ifd, fType, fCount, fOffset, origin uint,
                                                lEndian bool ) error {
    if ifd != _EXIF {
        return fmt.Errorf( "DigitalZoomRatio: tag used outside EXIF IFD\n" )
    }
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

func (jpg *JpegDesc) checkExifFocalLengthIn35mmFilm( ifd, fType, fCount, fOffset, origin uint,
                                                     lEndian bool ) error {
    if ifd != _EXIF {
        return fmt.Errorf( "FocalLengthIn35mmFilm: tag used outside EXIF IFD\n" )
    }
    return jpg.checkTiffUnsignedShort( "FocalLengthIn35mmFilm", lEndian, fType, fCount,
                                       fOffset, origin, nil )
}

func (jpg *JpegDesc) checkExifSceneCaptureType( ifd, fType, fCount, fOffset, origin uint,
                                                lEndian bool ) error {
    if ifd != _EXIF {
        return fmt.Errorf( "SceneCaptureType: tag used outside EXIF IFD\n" )
    }
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

func (jpg*JpegDesc) checkExifLensSpecification( ifd, fType, fCount, fOffset, origin uint,
                                                lEndian bool ) error {
// LensSpecification is an array of ordered rational values:
//  minimum focal length
//  maximum focal length
//  minimum F number in minimum focal length
//  maximum F number in maximum focal length
//  which are specification information for the lens that was used in photography.
//  When the minimum F number is unknown, the notation is 0/0.
    if ifd != _EXIF {
        return fmt.Errorf( "LensSpecification: tag used outside EXIF IFD\n" )
    }
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

func (jpg *JpegDesc) checkTiffField( ifd, tag, fType, fCount, fOffset, origin uint,
                                     lEndian bool ) error {

//    if tag < 0xfe {
//        return fmt.Errorf( "checkTiffField: unkown tag (%#02x)\n", tag )
//    }
    // FIXME: check if valid in ifd
    switch tag {
    case _Compression:
        return jpg.checkTiffCompression( ifd, fType, fCount, fOffset, origin, lEndian )
    case _Make:
        return jpg.checkTiffPrimaryThumbnailAscii( "Make", ifd, fType, fCount, fOffset, origin, lEndian )
    case _Model:
        return jpg.checkTiffPrimaryThumbnailAscii( "Model", ifd, fType, fCount, fOffset, origin, lEndian )

    case _Orientation:
        return jpg.checkTiffOrientation( ifd, fType, fCount, fOffset, origin, lEndian )
    case _XResolution:
        return jpg.checkTiffResolution( "XResolution", ifd, fType, fCount, fOffset, origin, lEndian )
    case _YResolution:
        return jpg.checkTiffResolution( "YResolution", ifd, fType, fCount, fOffset, origin, lEndian )

    case _ResolutionUnit:
        return jpg.checkTiffResolutionUnit( ifd, fType, fCount, fOffset, origin, lEndian )
    case _Software:
        return jpg.checkTiffPrimaryThumbnailAscii( "Software", ifd, fType, fCount, fOffset, origin, lEndian )
    case _DateTime:
        return jpg.checkTiffPrimaryThumbnailAscii( "Date", ifd, fType, fCount, fOffset, origin, lEndian )

    case _YCbCrPositioning:
        return jpg.checkTiffYCbCrPositioning( ifd, fType, fCount, fOffset, origin, lEndian )
    case _Copyright:
        return jpg.checkTiffPrimaryThumbnailAscii( "Copyright", ifd, fType, fCount, fOffset, origin, lEndian )

    case _ExposureTime:
        return jpg.checkExifExposureTime( ifd, fType, fCount, fOffset, origin, lEndian )
    case _FNumber:
        return jpg.checkExifFNumber( ifd, fType, fCount, fOffset, origin, lEndian )
    case _ExposureProgram:
        return jpg.checkExifExposureProgram( ifd, fType, fCount, fOffset, origin, lEndian )

    case _ISOSpeedRatings:
        return jpg.checkExifISOSpeedRatings( ifd, fType, fCount, fOffset, origin, lEndian )
    case _ExifVersion:
        return jpg.checkExifVersion( ifd, fType, fCount, fOffset, origin, lEndian )

    case _DateTimeOriginal:
        return jpg.checkExifAscii( "DateTimeOriginal", ifd, fType, fCount, fOffset, origin, lEndian )
    case _DateTimeDigitized:
        return jpg.checkExifAscii( "DateTimeDigitized", ifd, fType, fCount, fOffset, origin, lEndian )

    case _ComponentsConfiguration:  // special case: tiff type is undefined, but it is bytes
        return jpg.checkExifComponentsConfiguration( ifd, fType, fCount, fOffset, origin, lEndian )
    case _ShutterSpeedValue:
        return jpg.checkExifShutterSpeedValue( ifd, fType, fCount, fOffset, origin, lEndian )
    case _ApertureValue:
        return jpg.checkExifApertureValue( ifd, fType, fCount, fOffset, origin, lEndian )

    case _BrightnessValue:
        return jpg.checkExifBrightnessValue( ifd, fType, fCount, fOffset, origin, lEndian )
    case _ExposureBiasValue:
        return jpg.checkExifExposureBiasValue( ifd, fType, fCount, fOffset, origin, lEndian )
    case _MeteringMode:
        return jpg.checkExifMeteringMode( ifd, fType, fCount, fOffset, origin, lEndian )

    case _Flash:
        return jpg.checkExifFlash( ifd, fType, fCount, fOffset, origin, lEndian )
    case _FocalLength:
        return jpg.checkExifFocalLength( ifd, fType, fCount, fOffset, origin, lEndian )
    case _SubjectArea:
        return jpg.checkExifSubjectArea( ifd, fType, fCount, fOffset, origin, lEndian )

    case _MakerNote:
        return jpg.checkExifMakerNote( ifd, fType, fCount, fOffset, origin, lEndian )
    case _SubsecTimeOriginal:
        return jpg.checkExifAscii( "SubsecTimeOriginal", ifd, fType, fCount, fOffset, origin, lEndian )
    case _SubsecTimeDigitized:
        return jpg.checkExifAscii( "SubsecTimeDigitized", ifd, fType, fCount, fOffset, origin, lEndian )
    case _FlashpixVersion:
        return jpg.checkFlashpixVersion( ifd, fType, fCount, fOffset, origin, lEndian )

    case _ColorSpace:
        return jpg.checkExifColorSpace( ifd, fType, fCount, fOffset, origin, lEndian )
    case _PixelXDimension:
        return jpg.checkExifDimension( "PixelXDimension", ifd, fType, fCount, fOffset, origin, lEndian )
    case _PixelYDimension:
        return jpg.checkExifDimension( "PixelYDimension", ifd, fType, fCount, fOffset, origin, lEndian )

    case _SensingMethod:
        return jpg.checkExifSensingMethod( ifd, fType, fCount, fOffset, origin, lEndian )
    case _SceneType:
        return jpg.checkExifSceneType( ifd, fType, fCount, fOffset, origin, lEndian )
    case _ExposureMode:
        return jpg.checkExifExposureMode( ifd, fType, fCount, fOffset, origin, lEndian )
    case _WhiteBalance:
        return jpg.checkExifWhiteBalance( ifd, fType, fCount, fOffset, origin, lEndian )
    case _DigitalZoomRatio:
        return jpg.checkExifDigitalZoomRatio( ifd, fType, fCount, fOffset, origin, lEndian )
    case _FocalLengthIn35mmFilm:
        return jpg.checkExifFocalLengthIn35mmFilm( ifd, fType, fCount, fOffset, origin, lEndian )

    case _SceneCaptureType:
        return jpg.checkExifSceneCaptureType( ifd, fType, fCount, fOffset, origin, lEndian )
    case _LensSpecification:
        return jpg.checkExifLensSpecification( ifd, fType, fCount, fOffset, origin, lEndian )
    case _LensMake:
        return jpg.checkExifAscii( "LensMake", ifd, fType, fCount, fOffset, origin, lEndian )
    case _LensModel:
        return jpg.checkExifAscii( "LensModel", ifd, fType, fCount, fOffset, origin, lEndian )
        
    }
    return fmt.Errorf( "checkTiffField: unknown or unsupported tag (%#02x) @offset %#04x count %d\n",
                       tag, fOffset, fCount )
}

const (
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

const (
    _InteroperabilityIndex  = 0x01
)

func (jpg *JpegDesc) checkPrivateTiffField( tag, fType, fCount, fOffset, origin uint,
                                            lEndian, gps bool ) error {
    // TODO
    return nil
}

func (jpg *JpegDesc) checkIFD( Ifd, IfdOffset, origin uint, tag1, tag2 int,
                               lEndian bool ) ( offset0, offset1, offset2 uint, err error) {

    IfdOffset += origin
    offset1 = 0
    offset2 = 0
    err = nil

    /*
        Image File Directory starts with the number of following directory entries (2 bytes)
        followed by that number of entries (12 bytes) and one extra offset to the next IFD
        (4 bytes)
    */
    nIfdEntries := jpg.getUnsignedShort( lEndian, IfdOffset )
    if jpg.Content {
//        fmt.Printf( "  IFD #%d %s @%#04x #entries %d\n", ifd,
//                    IfdNames[Ifd], IfdOffset, nIfdEntries )
        fmt.Printf( "  %s:\n", IfdNames[Ifd] )
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
            err := jpg.checkTiffField( Ifd, tiffTag, tiffType, tiffCount,
                                       IfdOffset + 8, origin, lEndian )
            if err != nil {
                return 0, 0, 0, fmt.Errorf( "TIFF: invalid field: %v\n", err )
            }
        }
        IfdOffset += 12
    }
    offset0 = jpg.getUnsignedLong( lEndian, IfdOffset )
    return
}

var IfdNames [4]string = [...]string{ "Primary Image data", "Thumbnail Image data", "Exif data", "GPS data" }

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

    if exifIFDOffset != 0 {
        _, _, _, err = jpg.checkIFD( _EXIF, exifIFDOffset, origin, -1, -1, lEndian )
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

