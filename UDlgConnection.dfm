object FormConnection: TFormConnection
  Left = 0
  Top = 0
  BorderIcons = [biSystemMenu]
  Caption = 'Connection settings'
  ClientHeight = 328
  ClientWidth = 584
  Color = clBtnFace
  Font.Charset = DEFAULT_CHARSET
  Font.Color = clWindowText
  Font.Height = -15
  Font.Name = 'Arial'
  Font.Style = []
  OldCreateOrder = False
  Position = poOwnerFormCenter
  OnCreate = FormCreate
  OnShow = FormShow
  PixelsPerInch = 96
  TextHeight = 17
  object dxLayoutControl: TdxLayoutControl
    Left = 0
    Top = 0
    Width = 584
    Height = 328
    Align = alClient
    TabOrder = 0
    LayoutLookAndFeel = DMMain.dxLayoutSkinLookAndFeel
    object cxTextEditSshHost: TcxTextEdit
      Left = 65
      Top = 51
      Properties.MaxLength = 64
      Style.HotTrack = False
      TabOrder = 0
      TextHint = 'yourserver.com'
      Width = 222
    end
    object cxTextEditSshPort: TcxTextEdit
      Left = 65
      Top = 84
      Hint = '22'
      Style.HotTrack = False
      TabOrder = 1
      TextHint = '22'
      Width = 121
    end
    object cxTextEditSshLogin: TcxTextEdit
      Left = 65
      Top = 117
      Properties.MaxLength = 64
      Style.HotTrack = False
      TabOrder = 2
      TextHint = 'smartuser'
      Width = 121
    end
    object cxTextEditBaseName: TcxTextEdit
      Left = 350
      Top = 51
      Properties.MaxLength = 64
      Style.HotTrack = False
      TabOrder = 7
      TextHint = 'db_smartnote'
      Width = 210
    end
    object cxTextEditBaseHost: TcxTextEdit
      Left = 350
      Top = 84
      Properties.MaxLength = 64
      Style.HotTrack = False
      TabOrder = 8
      TextHint = '127.0.0.1'
      Width = 210
    end
    object cxTextEditBasePort: TcxTextEdit
      Left = 350
      Top = 117
      Style.HotTrack = False
      TabOrder = 9
      TextHint = '3306'
      Width = 210
    end
    object cxTextEditBaseLogin: TcxTextEdit
      Left = 350
      Top = 150
      Properties.MaxLength = 64
      Style.HotTrack = False
      TabOrder = 10
      TextHint = 'smartuser'
      Width = 210
    end
    object cxTextEditBasePwd: TcxTextEdit
      Left = 350
      Top = 183
      Properties.EchoMode = eemPassword
      Properties.MaxLength = 64
      Style.HotTrack = False
      TabOrder = 11
      Width = 210
    end
    object cxButtonOk: TcxButton
      Left = 13
      Top = 285
      Width = 78
      Height = 30
      Caption = 'OK'
      TabOrder = 13
      Font.Charset = DEFAULT_CHARSET
      Font.Color = clWindowText
      Font.Height = -15
      Font.Name = 'Arial'
      Font.Style = []
      ParentFont = False
      OnClick = cxButtonOkClick
    end
    object cxButtonCancel: TcxButton
      Left = 487
      Top = 285
      Width = 84
      Height = 30
      Caption = 'Cancel'
      ModalResult = 2
      TabOrder = 15
      Font.Charset = DEFAULT_CHARSET
      Font.Color = clWindowText
      Font.Height = -15
      Font.Name = 'Arial'
      Font.Style = []
      ParentFont = False
      OnClick = cxButtonCancelClick
    end
    object cxTextEditSshPwd: TcxTextEdit
      Left = 10000
      Top = 10000
      Properties.EchoMode = eemPassword
      Properties.MaxLength = 64
      Style.HotTrack = False
      TabOrder = 6
      Visible = False
      Width = 179
    end
    object cxButtonGenKey: TcxButton
      Left = 42
      Top = 202
      Width = 63
      Height = 28
      Caption = 'New'
      OptionsImage.Glyph.Data = {
        36040000424D3604000000000000360000002800000010000000100000000100
        2000000000000004000000000000000000000000000000000000000000000000
        0000000000000000000000000000000000010000000900000012000000120000
        0009000000020000000000000000000000000000000000000000000000000000
        0000000000030000000A0000000A0000000B2519156F68473BFF67443BFF1D13
        0F700000000C0000000B0000000C000000050000000100000000000000000000
        00030403021A543B33C738251EB30D0907454C362DC4B5A198FFAB958AFF3E29
        23C50D080748311F1AB13A241ECE030202220000000400000000000000000000
        00085A4138C9C7B9B0FFA8958BFF6C4F47FF715044FECABAB0FFC6B5AAFF6142
        37FE6B5047FE9C857AFFA89388FF3B241FCB0000000B00000000000000000000
        00074C3930ABC0AEA7FFE3DBD5FFD2C3BAFFC1AFA6FFCFC0B7FFCEBEB5FFBCAA
        9FFFCAB9AEFFC9B8ADFF9F897FFF36221DB20000000A00000000000000010000
        0007110C0B368D6F65FCEAE3DFFFDACDC6FFD8CCC3FFE3D9D3FFE7E0DAFFE1D6
        D0FFD2C3B9FFCDBCB3FF705449FE0D0907410000000A00000002000000042A1E
        1A6B4C372EBB846356FED9CEC8FFE3DAD5FFCDBEB7FF8E6F62FF947567FFD0C0
        B9FFE3DAD3FFC8B7ADFF6A4940FF47312AC22418147100000007000000088D6D
        60FFD8CCC7FFEEE8E5FFEBE6E0FFEAE3DDFF7F6257FF2219155E241B175E9374
        67FFEBE4DFFFD6C8C0FFD3C6BDFFB9A59BFF78574BFF0000000D000000079373
        64FFD9CECAFFF6F3F2FFFAF7F6FFEFEAE5FF76574DFF1D1411551E1613568869
        5DFFEFE9E6FFDDD2CBFFD8CAC3FFC7B8B1FF7E5B4FFF0000000C000000033428
        245F665046B5937467FEECE6E3FFF0ECE8FFCEC2BDFF856961FF886D65FFD2C7
        C2FFECE4DFFFD9CFC9FF856458FD5A443AB92B201C6102010108000000000000
        000316110F2EA78B7EFDF6F4F1FFF3EFECFFF2EEEAFFF3EFECFFF0EBE7FFEFEA
        E6FFEFEBE7FFECE5E0FF83665BFD110C0B390000000600000001000000000000
        0003685349AADCD1CBFFF8F6F3FFF8F5F3FFEBE4E1FFFDFCFBFFF3EEECFFE2D8
        D3FFFAF9F8FFF9F7F5FFC8B9B3FF4E3931B00000000500000000000000000000
        0002776055BDE5DDD8FFDCD0CBFFAC9183FD9E7E6EFDFBF8F8FFF5F1EEFF9979
        6AFDA3897EFCD7CBC5FFE0D6D1FF6E564CC20000000500000000000000000000
        00010605040D7C6458BF69544BA51713102B6F594EB2E1D7D2FFE0D6D1FF6D57
        4DB314100E2B614D44A4775E53C7050404110000000100000000000000000000
        0000000000000000000100000002000000023B302A5EA68676FFA58375FF3A2F
        295F000000030000000300000003000000010000000000000000000000000000
        0000000000000000000000000000000000000000000100000002000000030000
        0001000000000000000000000000000000000000000000000000}
      TabOrder = 3
      Font.Charset = DEFAULT_CHARSET
      Font.Color = clWindowText
      Font.Height = -15
      Font.Name = 'Arial'
      Font.Style = []
      ParentFont = False
      OnClick = cxButtonGenKeyClick
    end
    object cxButtonImportKey: TcxButton
      Left = 107
      Top = 202
      Width = 74
      Height = 28
      Caption = 'Import'
      OptionsImage.Glyph.Data = {
        36040000424D3604000000000000360000002800000010000000100000000100
        2000000000000004000000000000000000000000000000000000000000000000
        0000000000000000000000000030000000330000003300000033000000330000
        0033000000330000003300000033000000330000003300000030000000000000
        00000000000000000000AE7B18F2B67E0EFFB47B09FFB47A08FFB47B08FFB47A
        08FFB47A08FFB47B08FFB47A08FFB47B09FFB67E0EFFAE7B18F2000000000000
        00000000000000000000B67E0EFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF
        FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFB67E0EFF000000000000
        00000000000000000000B47B08FFFFFFFFFFFFFFFFFFFFFFFFFFD6BDA2FFFFFF
        FFFFFFFFFFFFD6BDA2FFFFFFFFFFFFFFFFFFFFFFFFFFB47B09FF000000000000
        00000000000000000000B57A04FFFFFFFFFFFFFFFBFFFFFFFDFFD7BC9EFFFFFF
        FEFFFFFFFEFFD6BC9EFFFFFFFCFFFFFFFAFFFFFFFFFFB47A08FF000000000000
        0000000000000000001AB77900FFFFFFFFFFDBBE99FFDCBF9CFFDBBF9DFFD8BB
        99FFD7BB99FFD8BD9CFFD7BB99FFD5B895FFFFFFFFFFB47B08FF000000000000
        00000000000059544A99AFA89AFFAAA69CFFA9A59AFFB0ACA1FFC6B299FFFFFF
        EFFFFFFFF0FFD9BC9AFFFFFFF5FFFFFDEDFFFFFFFFFFB47B08FF000000000000
        000000000000B0ADA5FFCCC5BDFFE4DAD3FFEAE0D9FFD0C8C0FFA9A49AFFFFFE
        E6FFFFFFEBFFDABF9EFFAD7200FFFFFFEFFFFFFFFFFFB57C09FF000000230000
        003300000033AEACA7FFCBC3BAFFDFD5CDFFE6DBD4FFD1C7BEFFACA69BFFE2BA
        8DFFDEBD99FFB07703FFF0BD6BFFAD7400FFFFFFFFFFB67E0FFF805B11C0B67E
        0CFFB67900FFABA9A3FFF7F1EDFFFEFAF4FFFEFAF3FFF9F3EDFFAFA89DFF49D2
        FFFFB67A03FFF0BE6CFFEDBC6BFFEFBD6BFFB27B0CFFB88217FFB67E0EFFFFFF
        FFFFFFFFFFFFA8A49AFFE2D9D0FFE9E0D7FFEAE1D8FFE4DCD4FFB0ADA9FFBC7E
        05FFF4BE67FFEEBB66FFEBB964FFEDBB67FFF1C06DFFB8841BFFB57C0AFFFFFF
        FFFFFFFFFFFFCAB7A0FFB1ACA2FFABA79EFFACAAA3FFB0AA9FFFB79248FFBB83
        13FFB88217FFB47F14FFEDB861FFB47F15FFB7831AFFAD7D1BEDB67D0BFFDFCA
        B9FFDEBB90FFE1BE93FFE4C094FFE5CEBAFFB97B00FF00000000000000000000
        002200000033B58116FFF0BE6AFFB68218FF0000000000000000B67D0AFFFFFF
        FFFF41C5FFFFE1B47FFF43C6FFFFDFFFFFFFB87C07FF00000000000000007D5A
        13BDB68117FFDCAB53FFEFC073FFB78319FF0000000000000000B67E0EFFFFFF
        FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFB77F0EFF0000000000000000B884
        1AFFF9CD8BFFF7CB88FFCD9D40FF7C5912AB0000000000000000AD7C17EFB67E
        0EFFB67C09FFB57B08FFB67C09FFB77E0EFFAD7C17EF0000000000000000805C
        13B1C3912EFFB78217FF7C5912AB000000000000000000000000}
      TabOrder = 4
      Font.Charset = DEFAULT_CHARSET
      Font.Color = clWindowText
      Font.Height = -15
      Font.Name = 'Arial'
      Font.Style = []
      ParentFont = False
      OnClick = cxButtonImportKeyClick
    end
    object cxButtonCopyPub: TcxButton
      Left = 184
      Top = 202
      Width = 75
      Height = 28
      Caption = 'Export'
      OptionsImage.Glyph.Data = {
        36040000424D3604000000000000360000002800000010000000100000000100
        2000000000000004000000000000000000000000000000000000000000000000
        0000000000000000000000000023000000330000000000000000000000000000
        0000000000170000003300000033000000330000003300000017000000000000
        00000000000000000000815D14C1B9851CFF0000003300000000000000000000
        0000504D488FB1AB9FFFAFA99DFFAFA99DFFB0AA9EFF504D488F000000000000
        00220000003300000033B7831AFFF2C16FFFB58014FF00000033000000200000
        0000AFABA1FFCCC5BCFFE5DAD3FFEBE1D9FFD2CAC1FFB0AB9EFF000000227C58
        12BCB78319FFB68218FFB37F15FFEEBC68FFF0BF6FFFB17906FFA1A1A1B40000
        0033AEABA4FFCBC3BAFFDFD5CDFFE6DBD4FFD1C8BFFFAFA99DFF7C5812BCCD9C
        40FFF3C479FFF0BD69FFEDB760FFEBB965FFEEBD6CFFF0BE6CFFB07500FFB476
        00FFABA8A2FFF7F1EDFFFEFAF4FFFEFAF3FFF8F3EDFFAEA89CFFB78218FFF6CB
        86FFDAA84DFFB27A0AFFB27C0EFFEDBB66FFF0BE6EFFB07704FFFFFFFFFFFFFF
        FFFFA8A49AFFE2D9D0FFE9E0D7FFEAE1D7FFE4DBD1FFAFAA9DFFBF8B26FFF7CA
        84FFB07703FFFFFFFFFFB27906FFEFBB63FFB27A09FFB17702FFFFFFFFFFFFFF
        FFFFCAB7A0FFB0ACA2FFAAA79EFFACA9A2FFAFABA1FF413F3B5DB78115FFB278
        04FFD1AD65FFFFFFFFFFC19442FFAF7400FFFFFFFFFFB17500FFDEC9B7FFDEBB
        90FFE1BE93FFE3BF94FFE4CEBAFFB87A00FF0000000000000000B67E0EFFFFFF
        FFFFFFFFFFFFFFFFFFFFD9C2A9FFFFFFFFFFFFFFFFFFB07200FFFFFFFFFF41C5
        FFFFE1B47FFF41C6FFFFFFFFFFFFB77B05FF0000000000000000B57B09FFFFFF
        FFFFD5B896FFD7BB99FFD9BE9EFFD8BD9DFFDAC0A5FFB07300FFFFFFFFFFFFFF
        FFFFFFFFFFFFFFFFFFFFFFFFFFFFB67E0DFF0000000000000000B47B08FFFFFF
        FFFFFFFBE8FFFFFDEDFFD7B995FFFFFFF0FFFFFFF6FFBF8F3AFFB07300FFAF71
        00FFB07300FFB37803FFB67E0DFF7F5B11AF0000000000000000B47B08FFFFFF
        FFFFFFF8DFFFFFFBE3FFD7B88EFFFFFCE5FFFFFDE8FFD9BC97FFFFFFF1FFFFFF
        EFFFFFFFFFFFB57C0BFF00000000000000000000000000000000B57B08FFFFFF
        FFFFDDB17FFFDFB584FFDEB789FFDFB686FFE0B686FFDEB88AFFE0B687FFDEB3
        83FFFFFFFFFFB57C09FF00000000000000000000000000000000B57C09FFFFFF
        FFFF44C4FFFF46C6FFFFE3B57EFF46C7FFFF46C7FFFFE3B57EFF46C6FFFF44C4
        FFFFFFFFFFFFB57C09FF00000000000000000000000000000000B67E0EFFFFFF
        FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF
        FFFFFFFFFFFFB67E0EFF00000000000000000000000000000000AD7C17EFB67E
        0EFFB67C09FFB67B08FFB57B08FFB67B08FFB67B08FFB57B08FFB67B08FFB67C
        09FFB67E0EFFAD7C17EF00000000000000000000000000000000}
      TabOrder = 5
      Font.Charset = DEFAULT_CHARSET
      Font.Color = clWindowText
      Font.Height = -15
      Font.Name = 'Arial'
      Font.Style = []
      ParentFont = False
      OnClick = cxButtonCopyPubClick
    end
    object cxButtonTest: TcxButton
      Left = 229
      Top = 285
      Width = 126
      Height = 30
      Caption = 'Test connection'
      TabOrder = 14
      Font.Charset = DEFAULT_CHARSET
      Font.Color = clWindowText
      Font.Height = -15
      Font.Name = 'Arial'
      Font.Style = []
      ParentFont = False
      OnClick = cxButtonTestClick
    end
    object cxTextEditSalt: TcxTextEdit
      Left = 10000
      Top = 10000
      Hint = 'Use same salt on all instances!'
      ParentShowHint = False
      Properties.EchoMode = eemPassword
      Properties.MaxLength = 64
      ShowHint = True
      Style.HotTrack = False
      TabOrder = 12
      Visible = False
      Width = 201
    end
    object dxLayoutControlGroup_Root: TdxLayoutGroup
      AlignHorz = ahClient
      AlignVert = avClient
      ButtonOptions.Buttons = <>
      Hidden = True
      ShowBorder = False
      Index = -1
    end
    object dxLayoutControlGroup6: TdxLayoutGroup
      Parent = dxLayoutControlGroup_Root
      AlignHorz = ahClient
      AlignVert = avClient
      CaptionOptions.Text = #1053#1086#1074#1086#1077' '#1089#1086#1077#1076#1080#1085#1077#1085#1080#1077
      ButtonOptions.Buttons = <>
      ButtonOptions.ShowExpandButton = True
      Hidden = True
      LayoutDirection = ldHorizontal
      ShowBorder = False
      Index = 0
    end
    object dxLayoutControlGroup1: TdxLayoutGroup
      Parent = dxLayoutControlGroup6
      AlignHorz = ahClient
      AlignVert = avClient
      CaptionOptions.Text = 'SSH'
      ButtonOptions.Buttons = <>
      ItemIndex = 3
      Index = 0
    end
    object dxLayoutControlItem1: TdxLayoutItem
      Parent = dxLayoutControlGroup1
      AlignHorz = ahClient
      CaptionOptions.Text = 'Host:'
      Control = cxTextEditSshHost
      ControlOptions.ShowBorder = False
      Index = 0
    end
    object dxLayoutControlItem2: TdxLayoutItem
      Parent = dxLayoutControlGroup1
      AlignHorz = ahClient
      CaptionOptions.Text = 'Port:'
      Control = cxTextEditSshPort
      ControlOptions.ShowBorder = False
      Index = 1
    end
    object dxLayoutControlItem3: TdxLayoutItem
      Parent = dxLayoutControlGroup1
      AlignHorz = ahClient
      CaptionOptions.Text = 'User:'
      Control = cxTextEditSshLogin
      ControlOptions.ShowBorder = False
      Index = 2
    end
    object dxLayoutControlGroup2: TdxLayoutGroup
      Parent = dxLayoutControlGroup6
      AlignHorz = ahClient
      CaptionOptions.Text = 'MySQL'
      ButtonOptions.Buttons = <>
      Index = 1
    end
    object dxLayoutControlItem13: TdxLayoutItem
      Parent = dxLayoutControlGroup2
      AlignHorz = ahClient
      CaptionOptions.Text = 'DB:'
      Control = cxTextEditBaseName
      ControlOptions.ShowBorder = False
      Index = 0
    end
    object dxLayoutControlItem4: TdxLayoutItem
      Parent = dxLayoutControlGroup2
      AlignHorz = ahClient
      CaptionOptions.Text = 'Host:'
      Control = cxTextEditBaseHost
      ControlOptions.ShowBorder = False
      Index = 1
    end
    object dxLayoutControlItem5: TdxLayoutItem
      Parent = dxLayoutControlGroup2
      AlignHorz = ahClient
      CaptionOptions.Text = 'Port:'
      Control = cxTextEditBasePort
      ControlOptions.ShowBorder = False
      Index = 2
    end
    object dxLayoutControlItem6: TdxLayoutItem
      Parent = dxLayoutControlGroup2
      AlignHorz = ahClient
      CaptionOptions.Text = 'User:'
      Control = cxTextEditBaseLogin
      ControlOptions.ShowBorder = False
      Index = 3
    end
    object dxLayoutControlItem10: TdxLayoutItem
      Parent = dxLayoutControlGroup2
      AlignHorz = ahClient
      CaptionOptions.Text = 'Pwd:'
      Control = cxTextEditBasePwd
      ControlOptions.ShowBorder = False
      Index = 4
    end
    object dxLayoutControlGroup3: TdxLayoutGroup
      Parent = dxLayoutControlGroup_Root
      AlignHorz = ahClient
      AlignVert = avBottom
      CaptionOptions.Text = 'New Group'
      ButtonOptions.Buttons = <>
      Hidden = True
      LayoutDirection = ldHorizontal
      ShowBorder = False
      Index = 1
    end
    object dxLayoutControlItem8: TdxLayoutItem
      Parent = dxLayoutControlGroup3
      CaptionOptions.Text = 'New Item'
      CaptionOptions.Visible = False
      Control = cxButtonOk
      ControlOptions.ShowBorder = False
      Index = 0
    end
    object dxLayoutControlItem9: TdxLayoutItem
      Parent = dxLayoutControlGroup3
      AlignHorz = ahRight
      CaptionOptions.Text = 'New Item'
      CaptionOptions.Visible = False
      Control = cxButtonCancel
      ControlOptions.ShowBorder = False
      Index = 2
    end
    object dxLayoutControlGroup4: TdxLayoutGroup
      Parent = dxLayoutControlGroup1
      AlignHorz = ahClient
      AlignVert = avClient
      CaptionOptions.Text = 'Authentication'
      CaptionOptions.Visible = False
      ButtonOptions.Buttons = <>
      ShowBorder = False
      Index = 3
    end
    object dxLayoutControlGroup7: TdxLayoutGroup
      Parent = dxLayoutControlGroup4
      CaptionOptions.Text = 'New Group'
      CaptionOptions.Visible = False
      Offsets.Top = 8
      ButtonOptions.Buttons = <>
      LayoutDirection = ldTabbed
      ShowBorder = False
      Index = 0
    end
    object dxLayoutControlItem7: TdxLayoutItem
      Parent = dxLayoutControlGroup8
      AlignHorz = ahClient
      AlignVert = avCenter
      CaptionOptions.Text = 'Pwd:'
      Control = cxTextEditSshPwd
      ControlOptions.ShowBorder = False
      Index = 0
    end
    object dxLayoutControlGroup5: TdxLayoutGroup
      Parent = dxLayoutControlGroup7
      AlignHorz = ahClient
      AlignVert = avClient
      CaptionOptions.Text = 'Keys'
      ButtonOptions.Buttons = <>
      LayoutDirection = ldHorizontal
      ShowBorder = False
      UseIndent = False
      Index = 0
    end
    object dxLayoutControlItem14: TdxLayoutItem
      Parent = dxLayoutControlGroup5
      AlignHorz = ahClient
      AlignVert = avCenter
      CaptionOptions.Text = 'New Item'
      CaptionOptions.Visible = False
      Control = cxButtonGenKey
      ControlOptions.ShowBorder = False
      Index = 0
    end
    object dxLayoutControlItem15: TdxLayoutItem
      Parent = dxLayoutControlGroup5
      AlignHorz = ahClient
      AlignVert = avCenter
      CaptionOptions.Text = 'New Item'
      CaptionOptions.Visible = False
      Offsets.Left = 1
      Offsets.Right = 1
      Control = cxButtonImportKey
      ControlOptions.ShowBorder = False
      Index = 1
    end
    object dxLayoutControlItem12: TdxLayoutItem
      Parent = dxLayoutControlGroup5
      AlignHorz = ahClient
      AlignVert = avCenter
      CaptionOptions.Text = 'New Item'
      CaptionOptions.Visible = False
      Control = cxButtonCopyPub
      ControlOptions.ShowBorder = False
      Index = 2
    end
    object dxLayoutControlGroup8: TdxLayoutGroup
      Parent = dxLayoutControlGroup7
      CaptionOptions.Text = 'Password'
      ButtonOptions.Buttons = <>
      Index = 1
    end
    object dxLayoutControlItem11: TdxLayoutItem
      Parent = dxLayoutControlGroup3
      AlignHorz = ahCenter
      AlignVert = avCenter
      CaptionOptions.Text = 'New Item'
      CaptionOptions.Visible = False
      Control = cxButtonTest
      ControlOptions.ShowBorder = False
      Index = 1
    end
    object dxLayoutControlItem16: TdxLayoutItem
      Parent = dxLayoutControlGroup9
      AlignHorz = ahClient
      AlignVert = avCenter
      CaptionOptions.Text = 'Salt:'
      Control = cxTextEditSalt
      ControlOptions.ShowBorder = False
      Index = 0
    end
    object dxLayoutControlGroup9: TdxLayoutGroup
      Parent = dxLayoutControlGroup2
      AlignHorz = ahClient
      AlignVert = avClient
      CaptionOptions.AlignHorz = taCenter
      CaptionOptions.Glyph.Data = {
        36040000424D3604000000000000360000002800000010000000100000000100
        2000000000000004000000000000000000000000000000000000000000000000
        0000000000000000000000000000000000010000000900000012000000120000
        0009000000020000000000000000000000000000000000000000000000000000
        0000000000030000000A0000000A0000000B2519156F68473BFF67443BFF1D13
        0F700000000C0000000B0000000C000000050000000100000000000000000000
        00030403021A543B33C738251EB30D0907454C362DC4B5A198FFAB958AFF3E29
        23C50D080748311F1AB13A241ECE030202220000000400000000000000000000
        00085A4138C9C7B9B0FFA8958BFF6C4F47FF715044FECABAB0FFC6B5AAFF6142
        37FE6B5047FE9C857AFFA89388FF3B241FCB0000000B00000000000000000000
        00074C3930ABC0AEA7FFE3DBD5FFD2C3BAFFC1AFA6FFCFC0B7FFCEBEB5FFBCAA
        9FFFCAB9AEFFC9B8ADFF9F897FFF36221DB20000000A00000000000000010000
        0007110C0B368D6F65FCEAE3DFFFDACDC6FFD8CCC3FFE3D9D3FFE7E0DAFFE1D6
        D0FFD2C3B9FFCDBCB3FF705449FE0D0907410000000A00000002000000042A1E
        1A6B4C372EBB846356FED9CEC8FFE3DAD5FFCDBEB7FF8E6F62FF947567FFD0C0
        B9FFE3DAD3FFC8B7ADFF6A4940FF47312AC22418147100000007000000088D6D
        60FFD8CCC7FFEEE8E5FFEBE6E0FFEAE3DDFF7F6257FF2219155E241B175E9374
        67FFEBE4DFFFD6C8C0FFD3C6BDFFB9A59BFF78574BFF0000000D000000079373
        64FFD9CECAFFF6F3F2FFFAF7F6FFEFEAE5FF76574DFF1D1411551E1613568869
        5DFFEFE9E6FFDDD2CBFFD8CAC3FFC7B8B1FF7E5B4FFF0000000C000000033428
        245F665046B5937467FEECE6E3FFF0ECE8FFCEC2BDFF856961FF886D65FFD2C7
        C2FFECE4DFFFD9CFC9FF856458FD5A443AB92B201C6102010108000000000000
        000316110F2EA78B7EFDF6F4F1FFF3EFECFFF2EEEAFFF3EFECFFF0EBE7FFEFEA
        E6FFEFEBE7FFECE5E0FF83665BFD110C0B390000000600000001000000000000
        0003685349AADCD1CBFFF8F6F3FFF8F5F3FFEBE4E1FFFDFCFBFFF3EEECFFE2D8
        D3FFFAF9F8FFF9F7F5FFC8B9B3FF4E3931B00000000500000000000000000000
        0002776055BDE5DDD8FFDCD0CBFFAC9183FD9E7E6EFDFBF8F8FFF5F1EEFF9979
        6AFDA3897EFCD7CBC5FFE0D6D1FF6E564CC20000000500000000000000000000
        00010605040D7C6458BF69544BA51713102B6F594EB2E1D7D2FFE0D6D1FF6D57
        4DB314100E2B614D44A4775E53C7050404110000000100000000000000000000
        0000000000000000000100000002000000023B302A5EA68676FFA58375FF3A2F
        295F000000030000000300000003000000010000000000000000000000000000
        0000000000000000000000000000000000000000000100000002000000030000
        0001000000000000000000000000000000000000000000000000}
      CaptionOptions.Text = 'Advanced notes encryption'
      Offsets.Top = 5
      ButtonOptions.Buttons = <>
      ButtonOptions.ShowExpandButton = True
      Expanded = False
      OnCollapsed = dxLayoutControlGroup9Collapsed
      OnExpanded = dxLayoutControlGroup9Expanded
      Index = 5
    end
  end
  object OpenDialog: TOpenDialog
    Title = 'Import private key'
    Left = 216
    Top = 152
  end
  object SaveDialog: TSaveDialog
    DefaultExt = 'pub'
    FileName = 'smartnote'
    Filter = 'RSA public key (*.pub)|*.pub'
    InitialDir = 'GetCurrentDir'
    Title = 'Export public key'
    Left = 160
    Top = 152
  end
  object UCTest: TUniConnection
    ProviderName = 'MySQL'
    Left = 416
    Top = 24
  end
  object MySQLUniProvider: TMySQLUniProvider
    Left = 416
    Top = 80
  end
  object ScSSHClientTest: TScSSHClient
    CiphersClient = <
      item
        Algorithm = saBlowfish
      end
      item
        Algorithm = saCast128
      end
      item
        Algorithm = saAES128
      end
      item
        Algorithm = saTripleDES
      end
      item
        Algorithm = saAES192
      end
      item
        Algorithm = saAES256
      end>
    CiphersServer = <
      item
        Algorithm = saBlowfish
      end
      item
        Algorithm = saCast128
      end
      item
        Algorithm = saAES128
      end
      item
        Algorithm = saTripleDES
      end
      item
        Algorithm = saAES192
      end
      item
        Algorithm = saAES256
      end>
    KeyStorage = DMMain.ScRegStorage
    OnServerKeyValidate = ScSSHClientTestServerKeyValidate
    Left = 136
    Top = 24
  end
  object ScSSHChannelTest: TScSSHChannel
    Client = ScSSHClientTest
    SourcePort = 3308
    DestHost = '127.0.0.1'
    DestPort = 3306
    Timeout = 5
    Left = 136
    Top = 88
  end
end
