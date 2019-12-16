object FormPwd: TFormPwd
  Left = 326
  Top = 230
  BorderIcons = [biSystemMenu]
  Caption = 'Set password'
  ClientHeight = 136
  ClientWidth = 274
  Color = clBtnFace
  Font.Charset = DEFAULT_CHARSET
  Font.Color = clWindowText
  Font.Height = -15
  Font.Name = 'Arial'
  Font.Style = []
  OldCreateOrder = False
  Position = poScreenCenter
  PixelsPerInch = 96
  TextHeight = 17
  object dxLayoutControl1: TdxLayoutControl
    Left = 0
    Top = 0
    Width = 274
    Height = 136
    Align = alClient
    TabOrder = 0
    LayoutLookAndFeel = DMMain.dxLayoutSkinLookAndFeel
    ExplicitWidth = 254
    ExplicitHeight = 138
    object cxTextEditPwd1: TcxTextEdit
      Left = 89
      Top = 13
      ParentFont = False
      Properties.EchoMode = eemPassword
      Properties.MaxLength = 32
      Properties.PasswordChar = '*'
      Properties.OnChange = cxTextEditPwd1PropertiesChange
      Style.Font.Charset = DEFAULT_CHARSET
      Style.Font.Color = clWindowText
      Style.Font.Height = -15
      Style.Font.Name = 'Arial'
      Style.Font.Style = []
      Style.HotTrack = False
      Style.LookAndFeel.NativeStyle = False
      Style.IsFontAssigned = True
      StyleDisabled.LookAndFeel.NativeStyle = False
      StyleFocused.LookAndFeel.NativeStyle = False
      StyleHot.LookAndFeel.NativeStyle = False
      TabOrder = 0
      TextHint = 'at least 6 symbols'
      Width = 149
    end
    object cxTextEditPwd2: TcxTextEdit
      Left = 89
      Top = 46
      ParentFont = False
      Properties.EchoMode = eemPassword
      Properties.MaxLength = 32
      Properties.PasswordChar = '*'
      Properties.OnChange = cxTextEditPwd1PropertiesChange
      Style.Font.Charset = DEFAULT_CHARSET
      Style.Font.Color = clWindowText
      Style.Font.Height = -15
      Style.Font.Name = 'Arial'
      Style.Font.Style = []
      Style.HotTrack = False
      Style.LookAndFeel.NativeStyle = False
      Style.IsFontAssigned = True
      StyleDisabled.LookAndFeel.NativeStyle = False
      StyleFocused.LookAndFeel.NativeStyle = False
      StyleHot.LookAndFeel.NativeStyle = False
      TabOrder = 1
      TextHint = 'at least 6 symbols'
      Width = 149
    end
    object cxButtonOK: TcxButton
      Left = 13
      Top = 93
      Width = 75
      Height = 30
      Caption = 'OK'
      LookAndFeel.NativeStyle = False
      ModalResult = 1
      TabOrder = 2
    end
    object cxButton2: TcxButton
      Left = 186
      Top = 93
      Width = 75
      Height = 30
      Caption = 'Cancel'
      LookAndFeel.NativeStyle = False
      ModalResult = 2
      TabOrder = 3
    end
    object dxLayoutControl1Group_Root: TdxLayoutGroup
      AlignHorz = ahClient
      AlignVert = avClient
      ButtonOptions.Buttons = <>
      Hidden = True
      ShowBorder = False
      Index = -1
    end
    object dxLayoutControl1Item1: TdxLayoutItem
      Parent = dxLayoutControl1Group_Root
      CaptionOptions.Text = 'Password:'
      Control = cxTextEditPwd1
      ControlOptions.ShowBorder = False
      Index = 0
    end
    object dxLayoutControl1Item2: TdxLayoutItem
      Parent = dxLayoutControl1Group_Root
      CaptionOptions.Text = 'Repeat:'
      Control = cxTextEditPwd2
      ControlOptions.ShowBorder = False
      Index = 1
    end
    object dxLayoutControl1Group1: TdxLayoutGroup
      Parent = dxLayoutControl1Group_Root
      AlignHorz = ahClient
      AlignVert = avBottom
      CaptionOptions.Text = 'New Group'
      ButtonOptions.Buttons = <>
      Hidden = True
      LayoutDirection = ldHorizontal
      ShowBorder = False
      Index = 2
    end
    object dxLayoutControl1Item4: TdxLayoutItem
      Parent = dxLayoutControl1Group1
      CaptionOptions.Text = 'New Item'
      CaptionOptions.Visible = False
      Control = cxButtonOK
      ControlOptions.ShowBorder = False
      Index = 0
    end
    object dxLayoutControl1Item3: TdxLayoutItem
      Parent = dxLayoutControl1Group1
      AlignHorz = ahRight
      CaptionOptions.Text = 'New Item'
      CaptionOptions.Visible = False
      Control = cxButton2
      ControlOptions.ShowBorder = False
      Index = 1
    end
  end
end
