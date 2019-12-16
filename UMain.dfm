object FormMain: TFormMain
  Left = 0
  Top = 0
  Caption = 'Smartnote'
  ClientHeight = 812
  ClientWidth = 1184
  Color = clBtnFace
  Font.Charset = DEFAULT_CHARSET
  Font.Color = clWindowText
  Font.Height = -15
  Font.Name = 'Arial'
  Font.Style = []
  OldCreateOrder = False
  Position = poScreenCenter
  OnClose = FormClose
  OnCreate = FormCreate
  OnShow = FormShow
  PixelsPerInch = 96
  TextHeight = 17
  object dxLayoutControl: TdxLayoutControl
    Left = 0
    Top = 0
    Width = 1184
    Height = 786
    Align = alClient
    TabOrder = 0
    LayoutLookAndFeel = DMMain.dxLayoutSkinLookAndFeel
    object cxGridNotes: TcxGrid
      Left = 13
      Top = 97
      Width = 244
      Height = 200
      BevelOuter = bvNone
      TabOrder = 2
      LookAndFeel.Kind = lfFlat
      object cxGridNotesDBTableView1: TcxGridDBTableView
        Navigator.Buttons.CustomButtons = <>
        OnFocusedRecordChanged = cxGridNotesDBTableView1FocusedRecordChanged
        DataController.DataSource = DMMain.LDSNotes
        DataController.KeyFieldNames = 'note_id'
        DataController.Summary.DefaultGroupSummaryItems = <>
        DataController.Summary.FooterSummaryItems = <>
        DataController.Summary.SummaryGroups = <>
        OptionsCustomize.ColumnFiltering = False
        OptionsCustomize.ColumnGrouping = False
        OptionsCustomize.ColumnHidingOnGrouping = False
        OptionsCustomize.ColumnMoving = False
        OptionsCustomize.ColumnSorting = False
        OptionsData.CancelOnExit = False
        OptionsData.Deleting = False
        OptionsData.DeletingConfirmation = False
        OptionsData.Editing = False
        OptionsData.Inserting = False
        OptionsView.ColumnAutoWidth = True
        OptionsView.GroupByBox = False
        object cxGridNotesDBTableView1ColumnNoteId: TcxGridDBColumn
          DataBinding.FieldName = 'note_id'
          Visible = False
          Width = 90
        end
        object cxGridNotesDBTableView1Column2: TcxGridDBColumn
          Caption = 'Name'
          DataBinding.FieldName = 'name'
          Width = 250
        end
        object cxGridNotesDBTableView1Column3: TcxGridDBColumn
          Caption = 'Updated'
          DataBinding.FieldName = 'updated'
          Visible = False
          Width = 150
        end
      end
      object cxGridNotesLevel1: TcxGridLevel
        GridView = cxGridNotesDBTableView1
      end
    end
    object cxMemoNoteInfo: TcxMemo
      Left = 276
      Top = 145
      Lines.Strings = (
        'cxMemoNoteInfo')
      ParentFont = False
      Properties.MaxLength = 8192
      Properties.ScrollBars = ssVertical
      Properties.OnChange = cxMemoNoteInfoPropertiesChange
      Style.Font.Charset = DEFAULT_CHARSET
      Style.Font.Color = clWindowText
      Style.Font.Height = -15
      Style.Font.Name = 'Arial'
      Style.Font.Style = []
      Style.HotTrack = False
      Style.IsFontAssigned = True
      TabOrder = 7
      Height = 89
      Width = 185
    end
    object cxTextEditPwd: TcxTextEdit
      Left = 523
      Top = 38
      BeepOnEnter = False
      Properties.EchoMode = eemPassword
      Style.HotTrack = False
      TabOrder = 0
      OnKeyDown = cxTextEditPwdKeyDown
      Width = 138
    end
    object cxButtonPwdSubmit: TcxButton
      Left = 669
      Top = 38
      Width = 68
      Height = 25
      Caption = 'Enter'
      TabOrder = 1
      OnClick = cxButtonPwdSubmitClick
    end
    object cxButtonApply: TcxButton
      Left = 276
      Top = 330
      Width = 124
      Height = 28
      Caption = 'Apply changes'
      TabOrder = 8
      OnClick = cxButtonApplyClick
    end
    object cxButtonNew: TcxButton
      Left = 626
      Top = 330
      Width = 80
      Height = 28
      Hint = 'Add a new note'
      Caption = 'New'
      OptionsImage.Glyph.Data = {
        36040000424D3604000000000000360000002800000010000000100000000100
        2000000000000004000000000000000000000000000000000000000000230000
        0033000000330000003300000033000000330000003300000033000000330000
        0033000000330000003300000033000000330000002B00000000286590C03890
        CEFF388FCEFF3990CEFF3A91CFFF3991CFFF3890CEFF368ECDFF388ED1FF3E91
        DCFF1A8F8AFF008C47FF008B49FF008C4AFF0F7964DE0000001E3890CEFF74DE
        FFFF5ED1FAFF4CC8F4FF3BAEE4FF55B0E3FF4FACE0FF7EE2FFFF83E3FFFF34B2
        9AFF009750FF00BB83FF77E0C6FF00BB85FF00995AFF00542EAB388FCEFF5ED1
        FAFF4AC5F2FF3BA9E0FF38BDF2FF3589C9FF77DFFFFF74DBFFFF7BDEFFFF0084
        36FF00BF88FF00BC83FFFFFFFFFF00BC83FF00C18DFF008C4CFF3990CEFF4CC9
        F4FF3BA9E0FF38BAEFFF47D5FFFF3588C8FF71DCFFFF6FD8FFFF77DCFFFF0083
        36FF75E6CBFFFFFFFFFFFFFFFFFFFFFFFFFF77E7CEFF008B49FF3A91CFFF3BAE
        E4FF38BDF2FF47D5FFFF67DFFFFF3487C8FF6BDAFFFF69D7FFFF70DAFFFF0083
        34FF00CB93FF00C98FFFFFFFFFFF00C990FF00CD98FF008C4AFF3991CFFF5AB1
        E3FF3689C9FF3688C8FF3587C8FF46A6DDFF64D8FFFF63D5FFFF68D8FFFF28A9
        95FF009D58FF00D29AFF75EED3FF00D49CFF00A363FF00522C943790CEFF59AF
        E0FF61DBFFFF62D9FFFF61D8FFFF60D7FFFF5FD4FFFF5ED3FFFF60D5FFFF67D8
        FFFF26A995FF008334FF008336FF008537FF198D88FF00000000358ECDFF96EB
        FFFF57D4FFFF59D3FFFF59D3FFFF59D3FFFF59D2FFFF59D2FFFF59D2FFFF5BD4
        FFFF5FD6FFFF61D7FFFF5FD8FFFF9DEDFFFF3B8ED8FF00000000338CCCFF99EA
        FFFF4FD1FFFF52D0FFFF53D1FFFF53D1FFFF53D1FFFF53D1FFFF53D1FFFF53D1
        FFFF54D1FFFF54D1FFFF50D2FFFF9BEAFFFF358DCEFF00000000338CCCFF9DEA
        FFFF47CEFFFF4ACEFFFF4BCFFFFF4BCFFFFF4BCFFFFF4BCFFFFF4BCFFFFF4BCF
        FFFF4BCFFFFF4ACEFFFF47CEFFFF9DEAFFFF338CCCFF00000000328CCCFFA2ED
        FFFF8CE4FFFF8FE4FFFF90E4FFFF90E4FFFF90E4FFFF90E4FFFF90E4FFFF90E4
        FFFF90E4FFFF8FE4FFFF8CE4FFFFA2EDFFFF328CCCFF00000000328CCCFFA9EF
        FFFF37B5EEFF3BB6EEFF3CB7EEFF3CB7EEFF3CB7EEFF3CB7EEFF3CB7EEFF3CB7
        EEFF3CB7EEFF3BB6EEFF37B5EEFFA9EFFFFF328CCCFF00000000338CCCFFB3F3
        FFFF37CDFFFF3BCDFFFF3CCEFFFF3CCEFFFF3CCEFFFF3CCEFFFF3CCEFFFF3CCE
        FFFF3CCEFFFF3BCDFFFF37CDFFFFB3F3FFFF338CCCFF00000000368ECEFF85EA
        FFFF85E7FFFF86E6FFFF87E6FFFF87E6FFFF87E6FFFF87E6FFFF87E6FFFF87E6
        FFFF87E6FFFF86E6FFFF85E7FFFF85EAFFFF368ECEFF000000003589C2EF368E
        CEFF358DCCFF358CCCFF358CCCFF358CCCFF358CCCFF358CCCFF358CCCFF358C
        CCFF358CCCFF358CCCFF358DCCFF368ECEFF3589C2EF00000000}
      ParentShowHint = False
      ShowHint = True
      TabOrder = 9
      OnClick = cxButtonNewClick
    end
    object cxButtonDelete: TcxButton
      Left = 714
      Top = 330
      Width = 96
      Height = 28
      Hint = 'Remove current note'
      Caption = 'Remove'
      OptionsImage.Glyph.Data = {
        36040000424D3604000000000000360000002800000010000000100000000100
        2000000000000004000000000000000000000000000000000000000000230000
        0033000000330000003300000033000000330000003300000033000000330000
        0033000000330000003300000033000000330000002B00000000286590C03890
        CEFF388FCEFF3990CEFF3A91CFFF3991CFFF3890CEFF368ECDFF3791CEFF3A9A
        D1FF2C5EBDFF212EACFF2131ACFF2231ADFF24459CDE0000001E3890CEFF74DE
        FFFF5ED1FAFF4CC8F4FF3BAEE4FF55B0E3FF4FACE0FF7EE3FFFF80E9FFFF497B
        D2FF2534BEFF395DFAFF3960FEFF395FFAFF283CC1FF141E68AB388FCEFF5ED1
        FAFF4AC5F2FF3BA9E0FF38BDF2FF3589C9FF77DFFFFF73DCFFFF77E6FFFF151B
        A4FF4162FBFF3D61FBFF3A5EF9FF3D61FBFF4366FDFF1E30ADFF3990CEFF4CC9
        F4FF3BA9E0FF38BAEFFF47D5FFFF3588C8FF71DCFFFF6EDAFFFF72E5FFFF131B
        A4FFAABAFFFFFFFFFFFFFFFFFFFFFFFFFFFFACBDFFFF1B2DACFF3A91CFFF3BAE
        E4FF38BDF2FF47D5FFFF67DFFFFF3487C8FF6BDAFFFF69D8FFFF6CE3FFFF141A
        A3FF5D76FDFF5877FFFF5473FEFF5877FFFF5F7BFFFF1D2EADFF3991CFFF5AB1
        E3FF3689C9FF3688C8FF3587C8FF46A6DDFF64D8FFFF63D6FFFF65DDFFFF3A72
        CEFF333EC2FF7085FFFF758CFFFF7087FFFF374AC7FF131C66943790CEFF59AF
        E0FF61DBFFFF62D9FFFF61D8FFFF60D7FFFF5FD4FFFF5ED4FFFF60D7FFFF63E0
        FFFF3873CEFF171BA3FF161DA4FF171EA6FF295CBCFF00000000358ECDFF96EB
        FFFF57D4FFFF59D3FFFF59D3FFFF59D3FFFF59D2FFFF59D2FFFF59D3FFFF5AD6
        FFFF5CDBFFFF5DDFFFFF5BE1FFFF9AF5FFFF3796CFFF00000000338CCCFF99EA
        FFFF4FD1FFFF52D0FFFF53D1FFFF53D1FFFF53D1FFFF53D1FFFF53D1FFFF53D1
        FFFF54D2FFFF53D3FFFF50D3FFFF9AECFFFF348ECDFF00000000338CCCFF9DEA
        FFFF47CEFFFF4ACEFFFF4BCFFFFF4BCFFFFF4BCFFFFF4BCFFFFF4BCFFFFF4BCF
        FFFF4BCFFFFF4ACEFFFF47CEFFFF9DEAFFFF338CCCFF00000000328CCCFFA2ED
        FFFF8CE4FFFF8FE4FFFF90E4FFFF90E4FFFF90E4FFFF90E4FFFF90E4FFFF90E4
        FFFF90E4FFFF8FE4FFFF8CE4FFFFA2EDFFFF328CCCFF00000000328CCCFFA9EF
        FFFF37B5EEFF3BB6EEFF3CB7EEFF3CB7EEFF3CB7EEFF3CB7EEFF3CB7EEFF3CB7
        EEFF3CB7EEFF3BB6EEFF37B5EEFFA9EFFFFF328CCCFF00000000338CCCFFB3F3
        FFFF37CDFFFF3BCDFFFF3CCEFFFF3CCEFFFF3CCEFFFF3CCEFFFF3CCEFFFF3CCE
        FFFF3CCEFFFF3BCDFFFF37CDFFFFB3F3FFFF338CCCFF00000000368ECEFF85EA
        FFFF85E7FFFF86E6FFFF87E6FFFF87E6FFFF87E6FFFF87E6FFFF87E6FFFF87E6
        FFFF87E6FFFF86E6FFFF85E7FFFF85EAFFFF368ECEFF000000003589C2EF368E
        CEFF358DCCFF358CCCFF358CCCFF358CCCFF358CCCFF358CCCFF358CCCFF358C
        CCFF358CCCFF358CCCFF358DCCFF368ECEFF3589C2EF00000000}
      ParentShowHint = False
      ShowHint = True
      TabOrder = 10
      OnClick = cxButtonDeleteClick
    end
    object cxButtonCancel: TcxButton
      Left = 1026
      Top = 330
      Width = 134
      Height = 28
      Caption = 'Cancel changes'
      TabOrder = 11
      OnClick = cxButtonCancelClick
    end
    object cxTextEditNoteName: TcxTextEdit
      Left = 325
      Top = 112
      Properties.OnChange = cxTextEditNoteNamePropertiesChange
      Style.HotTrack = False
      TabOrder = 3
      Text = 'cxTextEditNoteName'
      Width = 250
    end
    object cxTextEditLen: TcxTextEdit
      Left = 976
      Top = 112
      Enabled = False
      Properties.Alignment.Horz = taCenter
      Style.HotTrack = False
      TabOrder = 5
      Text = '1055/8192'
      Width = 88
    end
    object cxDateEditUpdated: TcxTextEdit
      Left = 742
      Top = 112
      Enabled = False
      Properties.Alignment.Horz = taCenter
      Style.HotTrack = False
      TabOrder = 4
      Text = 'cxDateEditUpdated'
      Width = 172
    end
    object cxButtonHistory: TcxButton
      Left = 1072
      Top = 112
      Width = 88
      Height = 25
      Caption = 'History'
      OptionsImage.Glyph.Data = {
        36040000424D3604000000000000360000002800000010000000100000000100
        2000000000000004000000000000000000000000000000000000000000000000
        0000000000000000002300000033000000330000003300000033000000330000
        0033000000330000003300000033000000330000003300000023000000000000
        000B0000000827638CBD368FCEFF358DCCFF358DCCFF358DCCFF358DCCFF358D
        CCFF358DCCFF358DCCFF358DCCFF358DCCFF368ECDFF26648FC00000002F0C1F
        2B5D09161F51368ECDFF86E7FFFF7BE0FFFF7BDFFFFF7BDFFFFF7BDFFFFF7BDF
        FFFF7BDFFFFF7BDFFFFF7BDFFFFF7AE0FFFF85E5FFFF47A3DCFF3588C1F1378F
        CEFF368ECDFF338BCBFF85E3FFFF6DD9FFFF6ED8FFFF6ED8FFFF6ED8FFFF6ED8
        FFFF6ED8FFFF6ED8FFFF6ED8FFFF6DD9FFFF84E2FFFF43A0DAFF368FCEFF87E8
        FFFF7EE4FFFF3087C9FF89E4FFFF64D5FFFF66D5FFFF66D5FFFF66D5FFFF66D6
        FFFF66D5FFFF66D5FFFF66D5FFFF64D5FFFF89E2FFFF409EDAFF358DCCFF86E5
        FFFF71DEFFFF2E86C8FF8EE6FFFF5DD4FFFF5FD4FFFF60D5FFFF5CCCF6FF5CCD
        F7FF5CCCF6FF60D5FFFF5FD4FFFF5DD4FFFF8EE5FFFF3E9EDAFF348CCCFF8BE5
        FFFF68DBFFFF2E85C7FF94E8FFFF52D1FFFF54D1FFFF55D2FFFF4DC3EDFF4BBE
        E7FF4DC3EDFF55D2FFFF54D1FFFF52D1FFFF94E8FFFF3B9EDAFF348CCCFF90E8
        FFFF60DAFFFF2E85C7FF9BE9FFFF91E5FFFF93E5FFFF94E6FFFF91DBF6FF8DD0
        E8FF91DBF6FF94E6FFFF93E5FFFF91E4FFFF9BE9FFFF399DDAFF338CCCFF96EA
        FFFF55D7FFFF2E85C8FFA6EDFFFF3AB7EEFF3EB7EEFF3FB8EFFF3CB3E8FF339B
        CBFF3DB5EBFF40BAF1FF3EB8EFFF3AB6EEFFA6ECFFFF379DDAFF338CCCFF9DEC
        FFFF95EAFFFF2E85C8FFB2F1FFFF39CEFFFF3DCFFFFF3ED0FFFF3DD3FFFF698B
        97FF3EB5DEFF40D7FFFF3FD3FFFF3AD0FFFFB3F1FFFF359DDAFF338CCCFFA7EE
        FFFF3CBAF1FF3186C8FF84E8FFFF83E5FFFF85E6FFFF85E7FFFF85E9FFFF89DF
        F9FF776D64FF5C9EA9FF8CF6FFFF88F1FFFF87EDFFFF379FDCFF338CCCFFB3F3
        FFFF3AD1FFFF35A4DFFF3086C8FF3087C9FF3089CCFF318CD0FF328ED0FF3290
        D1FF4D8BACFF0A009CFF0000A7FF0001ADFF3A99CFFF28658EAF368ECEFF85EB
        FFFF85E8FFFF86EAFFFF87EBFFFF86EDFFFF8BE1F8FF797165FF5CA3ADFF8EFF
        FFFF7EE3FFFF0000AAFF656EF7FF0000AAFF000000000000000027658FB0368E
        CEFF358DCCFF358DCCFF358ECDFF368FCEFF3796D3FF5395B0FF0D029EFF0000
        A9FF0312B1FF081AB4FF0000ADFF0000A1EE0000000000000000000000000000
        00000000000000000000000000000000000000000000000000000000ADFF6772
        F8FF0000ACFF0000000000000000000000000000000000000000000000000000
        00000000000000000000000000000000000000000000000000000000A2ED0000
        AEFF0000A2EE0000000000000000000000000000000000000000}
      TabOrder = 6
      OnClick = cxButtonHistoryClick
    end
    object cxButtonNewStorage: TcxButton
      Left = 532
      Top = 691
      Width = 118
      Height = 30
      Caption = 'Create new'
      OptionsImage.Glyph.Data = {
        36040000424D3604000000000000360000002800000010000000100000000100
        2000000000000004000000000000000000000000000000000000000000000000
        0001000000040000000B00000011000000160000001A0000001B0000001C0000
        001C00000018000000150000000E000000050000000100000000000000000000
        000504091032122749931A386AC61C417DE2214B95FF1F4893FF1E4692FF1C44
        91FF17397AE6122D63CA0D20479E03070F390000000700000000000000000000
        000F2A558EE8619CC8FFA6C6E0FFD2E0EFFFA7E7FAFF6CD1F1FF44BDE6FF3F98
        D9FF2565B8FF2966B7FF3174BBFF1E427DE20000001500000000000000000000
        00133365A6FF9FE2F7FFDBF6FDFFF3FCFFFFABE9FAFF71D4F2FF49C0E8FF449D
        DAFF296DC1FF2E74C6FF419DE1FF275298FF0000001B00000000000000000000
        0012396CAAFFADE7F9FFE3F8FDFFF7FCFFFFC1EFFBFF8FDFF6FF67CEEDFF5EAF
        E2FF3B7ECAFF3A80CCFF49A3E4FF29579BFF0000001B00000000000000000000
        00114277B1FF91CBE7FFAECDE3FFB8C9E0FF6FADD6FF469BCEFF2E8BC5FF2B6F
        B9FF2155ABFF2662B4FF3B8AD2FF2D5B9DFF0000001A00000000000000000000
        000B2C5588CE89C6E4FFCAE4F2FFE8F2F9FFB6EDFCFF80DAF4FF59C9ECFF54A9
        E0FF3374C4FF3274C3FF3C8BD2FF21467CCE0000001100000000000000000000
        000E437CB6FFB3E9F9FFE3F8FDFFF7FCFFFFBAEDFCFF86DCF5FF5FCCECFF59AD
        E2FF3C82CDFF4288D1FF56ADE7FF3366A6FF0000001700000000000000000000
        000D4984BCFFC0EDFAFFEAFAFDFFF9FCFFFFCDF3FDFFA1E5F7FF7DD8F1FF73BD
        E8FF4F92D5FF4E95D7FF5FB3EAFF386BAAFF0000001600000000000000000000
        000C528EC2FFA1D0E9FFB5CEE3FFBAC9E0FF7BB2D7FF54A2D1FF3B95CAFF387C
        C0FF2C65B5FF3470BEFF4C99D8FF3A70AEFF0000001400000000000000000000
        000A366795CE98CBE7FFD0E6F3FFEBF3F9FFC4F0FCFF94E1F6FF6FD4EFFF69B8
        E7FF4587CEFF4486CCFF4C99D8FF2C5689CE0000000E00000000000000000000
        00095394C8FFC2EEFAFFE8F9FEFFF8FDFFFFC6F1FCFF97E3F7FF73D5F0FF6EBB
        E8FF4F94D6FF559BDAFF6BBBECFF437DB7FF0000001100000000000000000000
        0008569ACBFFC6EFFBFFE9FAFEFFF8FDFFFFC9F2FCFF9CE4F7FF78D7F1FF73BE
        E9FF5498D8FF5A9FDCFF70BEEDFF4883BBFF0000001000000000000000000000
        00075A9FD0FFE3F8FCFFF3FCFEFFF3FDFEFFEFFBFEFFE8F9FEFFE1F7FDFFDAF4
        FDFFC5E9FAFFB2DFF7FFA4DDF6FF4B89BFFF0000000E00000000000000000000
        00045291BAE3A4CDE7FFC3DFEFFFCFE9F5FFE2F8FDFFD8F5FDFFCEF1FDFFC2ED
        FBFFA5D8F2FF97CEECFF81BBE1FF4277A4DB0000000A00000000000000000000
        00010910141D2D4F647E417191B34B83A9D05BA0D0FC5AA1D1FF5A9FD0FF579B
        CCFC467CA4D23C6A8DBA2A4B648A091015270000000300000000}
      TabOrder = 12
      OnClick = cxButtonNewStorageClick
    end
    object dxLayoutControlGroup_Root: TdxLayoutGroup
      AlignHorz = ahClient
      AlignVert = avClient
      ButtonOptions.Buttons = <>
      Hidden = True
      ShowBorder = False
      TabbedOptions.HideTabs = True
      TabbedOptions.ShowFrame = True
      Index = -1
    end
    object dxLayoutControlGroupHotMenu: TdxLayoutGroup
      Parent = dxLayoutControlGroup_Root
      AlignHorz = ahClient
      AlignVert = avClient
      CaptionOptions.Text = 'Hot menu'
      ButtonOptions.Buttons = <>
      ItemIndex = 1
      LayoutDirection = ldHorizontal
      ShowBorder = False
      Index = 1
    end
    object dxLayoutControlGroup3: TdxLayoutGroup
      Parent = dxLayoutControlGroupHotMenu
      AlignHorz = ahClient
      AlignVert = avClient
      ButtonOptions.Buttons = <>
      Index = 1
    end
    object dxLayoutControlItemNotes: TdxLayoutItem
      Parent = dxLayoutControlGroupHotMenu
      AlignVert = avClient
      CaptionOptions.Text = 'New Item'
      CaptionOptions.Visible = False
      Control = cxGridNotes
      ControlOptions.ShowBorder = False
      Index = 0
    end
    object dxLayoutControlItemMemo: TdxLayoutItem
      Parent = dxLayoutControlGroup3
      AlignVert = avClient
      CaptionOptions.Text = 'New Item'
      CaptionOptions.Visible = False
      Control = cxMemoNoteInfo
      ControlOptions.ShowBorder = False
      Index = 1
    end
    object dxLayoutControlGroupDefault: TdxLayoutGroup
      Parent = dxLayoutControlGroup_Root
      AlignHorz = ahClient
      AlignVert = avClient
      CaptionOptions.Text = 'Default'
      ButtonOptions.Buttons = <>
      LayoutDirection = ldHorizontal
      ShowBorder = False
      Index = 0
    end
    object dxLayoutControlGroup5: TdxLayoutGroup
      Parent = dxLayoutControlGroupDefault
      AlignHorz = ahCenter
      AlignVert = avCenter
      CaptionOptions.Visible = False
      ButtonOptions.Buttons = <>
      LayoutDirection = ldHorizontal
      Index = 0
    end
    object dxLayoutControlItem1: TdxLayoutItem
      Parent = dxLayoutControlGroup5
      AlignVert = avCenter
      CaptionOptions.Text = 'Password:'
      Control = cxTextEditPwd
      ControlOptions.ShowBorder = False
      Index = 0
    end
    object dxLayoutControlItem2: TdxLayoutItem
      Parent = dxLayoutControlGroup5
      AlignVert = avCenter
      CaptionOptions.Text = 'New Item'
      CaptionOptions.Visible = False
      Control = cxButtonPwdSubmit
      ControlOptions.ShowBorder = False
      Index = 1
    end
    object dxLayoutControlGroup1: TdxLayoutGroup
      Parent = dxLayoutControlGroup3
      AlignHorz = ahClient
      AlignVert = avBottom
      CaptionOptions.Text = 'New Group'
      ButtonOptions.Buttons = <>
      LayoutDirection = ldHorizontal
      ShowBorder = False
      Index = 2
    end
    object dxLayoutControlItemApply: TdxLayoutItem
      Parent = dxLayoutControlGroup1
      AlignHorz = ahLeft
      AlignVert = avCenter
      CaptionOptions.Text = 'New Item'
      CaptionOptions.Visible = False
      Control = cxButtonApply
      ControlOptions.ShowBorder = False
      Index = 0
    end
    object dxLayoutControlGroup2: TdxLayoutGroup
      Parent = dxLayoutControlGroup1
      AlignHorz = ahCenter
      AlignVert = avCenter
      CaptionOptions.Text = 'New Group'
      ButtonOptions.Buttons = <>
      Hidden = True
      LayoutDirection = ldHorizontal
      ShowBorder = False
      Index = 1
    end
    object dxLayoutControlItemNew: TdxLayoutItem
      Parent = dxLayoutControlGroup2
      CaptionOptions.Text = 'New Item'
      CaptionOptions.Visible = False
      Control = cxButtonNew
      ControlOptions.ShowBorder = False
      Index = 0
    end
    object dxLayoutControlItemRemove: TdxLayoutItem
      Parent = dxLayoutControlGroup2
      CaptionOptions.Text = 'New Item'
      CaptionOptions.Visible = False
      Control = cxButtonDelete
      ControlOptions.ShowBorder = False
      Index = 1
    end
    object dxLayoutControlItemCancel: TdxLayoutItem
      Parent = dxLayoutControlGroup1
      AlignHorz = ahRight
      AlignVert = avCenter
      CaptionOptions.Text = 'New Item'
      CaptionOptions.Visible = False
      Control = cxButtonCancel
      ControlOptions.ShowBorder = False
      Index = 2
    end
    object dxLayoutControlGroup4: TdxLayoutGroup
      Parent = dxLayoutControlGroup3
      CaptionOptions.Text = 'New Group'
      CaptionOptions.Visible = False
      ButtonOptions.Buttons = <>
      Hidden = True
      LayoutDirection = ldHorizontal
      ShowBorder = False
      Index = 0
    end
    object dxLayoutControlItemName: TdxLayoutItem
      Parent = dxLayoutControlGroup4
      AlignHorz = ahClient
      AlignVert = avCenter
      CaptionOptions.Text = 'Name:'
      Control = cxTextEditNoteName
      ControlOptions.ShowBorder = False
      Index = 0
    end
    object dxLayoutControlItemLen: TdxLayoutItem
      Parent = dxLayoutControlGroup4
      AlignHorz = ahRight
      AlignVert = avCenter
      CaptionOptions.Text = 'Length:'
      Control = cxTextEditLen
      ControlOptions.ShowBorder = False
      Enabled = False
      Index = 2
    end
    object dxLayoutControlItemUpdated: TdxLayoutItem
      Parent = dxLayoutControlGroup4
      AlignHorz = ahRight
      AlignVert = avCenter
      CaptionOptions.Text = 'Updated:'
      Control = cxDateEditUpdated
      ControlOptions.ShowBorder = False
      Enabled = False
      Index = 1
    end
    object dxLayoutControlItemHistory: TdxLayoutItem
      Parent = dxLayoutControlGroup4
      AlignHorz = ahRight
      AlignVert = avCenter
      CaptionOptions.Text = 'New Item'
      CaptionOptions.Visible = False
      Control = cxButtonHistory
      ControlOptions.ShowBorder = False
      Index = 3
    end
    object dxLayoutControlGroupStorage: TdxLayoutGroup
      Parent = dxLayoutControlGroup_Root
      AlignHorz = ahClient
      AlignVert = avClient
      CaptionOptions.Text = 'New Group'
      CaptionOptions.Visible = False
      ButtonOptions.Buttons = <>
      LayoutDirection = ldHorizontal
      Index = 4
    end
    object dxLayoutControlGroup7: TdxLayoutGroup
      Parent = dxLayoutControlGroupStorage
      AlignHorz = ahCenter
      AlignVert = avCenter
      CaptionOptions.Text = 'New Group'
      CaptionOptions.Visible = False
      ButtonOptions.Buttons = <>
      Padding.Left = 10
      Padding.Right = 10
      Padding.AssignedValues = [lpavLeft, lpavRight]
      Index = 0
    end
    object dxLayoutControlLabeledItem1: TdxLayoutLabeledItem
      Parent = dxLayoutControlGroup7
      AlignHorz = ahCenter
      CaptionOptions.Text = 'Local storage does not exist.'
      Index = 0
    end
    object dxLayoutControlItem3: TdxLayoutItem
      Parent = dxLayoutControlGroup7
      AlignHorz = ahCenter
      CaptionOptions.Text = 'New Item'
      CaptionOptions.Visible = False
      Control = cxButtonNewStorage
      ControlOptions.ShowBorder = False
      Index = 1
    end
    object dxLayoutControlGroupConnect: TdxLayoutGroup
      Parent = dxLayoutControlGroup_Root
      AlignHorz = ahClient
      AlignVert = avClient
      CaptionOptions.Text = 'New Group'
      CaptionOptions.Visible = False
      ButtonOptions.Buttons = <>
      LayoutDirection = ldHorizontal
      Index = 2
    end
    object dxLayoutControlLabeledItemInfo: TdxLayoutLabeledItem
      Parent = dxLayoutControlGroup6
      AlignHorz = ahCenter
      AlignVert = avCenter
      CaptionOptions.Glyph.Data = {
        36040000424D3604000000000000360000002800000010000000100000000100
        2000000000000004000000000000000000000000000000000000000000000000
        00000000000B0000002200000031000000330000003300000033000000330000
        00330000003300000031000000220000000B0000000000000000000000000000
        00172524215D757169B9AAA498F7AFA99DFFAEA99CFFAEA89CFFAEA89CFFAEA8
        9CFFAFA99DFFAAA498F7757169B92524215D0000001700000000000000004F4C
        478DB4ADA2FFC2BBB0FFD0C7BEFFDFD5CDFFE6DCD4FFEDE1DBFFEFE4DDFFE7DD
        D6FFE1D6CFFFD1C9BFFFC3BCB1FFB4AEA2FF4F4C478D0000000000000000B1AB
        9FFFC7C0B6FFCFC7BEFFD5CCC3FFDBD2C9FFE2D8D0FFE9DDD7FFEBE0D9FFE3D9
        D2FFDDD3CBFFD6CEC5FFD1C9C0FFC9C2B9FFB1AB9FFF0000000000000000B1AB
        9FFFC9C0B8FFCDC5BCFFD4CBC2FFDAD1C8FFE1D7CFFFE7DCD5FFEADFD8FFE2D8
        D1FFDCD2CAFFD5CDC4FFCFC7BEFFCAC3B9FFB1AB9FFF0000000000000000B0AB
        9EFFC8BFB7FFCBC3BAFFD2C9C0FFD8CFC5FFDFD5CDFFE9DFD8FFE9DDD6FFE0D6
        CFFFDAD0C8FFD3CBC2FFCEC6BDFFC9C2B7FFB0AA9EFF0000000000000000B0AA
        9EFFC9C2B9FFE2DDD4FFF5EFE9FFFFFBF4FFFEFAF3FFFDF9F2FFFDF9F2FFFDFA
        F3FFFFFBF4FFF6F0E8FFE4DDD5FFCBC4BAFFB0AA9EFF00000000000000006F6B
        64ADF9F4EEFFD7D0C8FFC6BEB6FFBEB5ABFFC2B9AFFFC7BCB4FFC8BEB5FFC3B9
        B1FFBFB6ACFFC7BFB7FFD8D1C9FFF9F5EDFF6F6B64AD0000000000000000706C
        64B4A3998FFFB6AEA4FFC8BFB6FFD8CFC7FFE3D9D1FFEADED8FFECE1DAFFE4DA
        D3FFDAD1C9FFCAC1B8FFB8AFA6FFA49C92FF706C64B40000000000000000B1AB
        9FFFCAC1B9FFCEC7BEFFD5CCC3FFDAD1C8FFE1D7CFFFE8DCD6FFEADFD8FFE2D8
        D1FFDCD2CAFFD6CEC5FFD1C9C0FFCBC4BAFFB1AB9FFF0000000000000000B1AB
        9EFFC8BFB7FFCBC3BAFFD2C8BFFFD8CEC5FFDFD5CDFFE9DED8FFE9DDD6FFE0D6
        CFFFDACFC7FFD3CBC2FFCEC6BDFFC9C2B8FFB0AB9EFF0000000000000000B0AA
        9DFFC7C0B8FFE1DBD2FFF3EDE6FFFDF9F2FFFCF8F1FFFBF7F0FFFBF7F0FFFCF8
        F1FFFDF8F2FFF4EEE6FFE2DBD3FFCAC3B9FFAFAA9DFF0000000000000000AEA8
        9CFFFBF5EFFFEEE7DEFFE3DCD3FFDDD5CCFFDDD5CBFFDDD4CBFFDDD4CBFFDDD4
        CBFFDDD5CBFFE3DCD3FFEEE7DEFFFBF6EFFFAEA89CFF0000000000000000AFA9
        9DFFE1D8CEFFE1D8CEFFDFD6CCFFDFD6CBFFDFD5CBFFDFD5CBFFDFD5CBFFDFD5
        CBFFDFD6CBFFDFD6CCFFE1D8CEFFE1D8CEFFAFA99DFF00000000000000004E4C
        4671B6B0A4FFD2CABFFFE2D9D0FFEFE7DDFFEEE6DCFFEEE6DCFFEEE6DCFFEEE6
        DCFFEFE7DDFFE2D9D0FFD2CABFFFB6B0A4FF4E4C467100000000000000000000
        000025242135747068A8A8A297F5AEA89CFFAEA89CFFAEA89CFFAEA89CFFAEA8
        9CFFAEA89CFFA8A297F5747068A8252421350000000000000000}
      CaptionOptions.Text = 'Connecting to remote storage...'
      Offsets.Left = 10
      Offsets.Right = 10
      Index = 0
    end
    object dxLayoutControlGroupSync: TdxLayoutGroup
      Parent = dxLayoutControlGroup_Root
      AlignHorz = ahClient
      AlignVert = avClient
      CaptionOptions.Text = 'New Group'
      CaptionOptions.Visible = False
      ButtonOptions.Buttons = <>
      LayoutDirection = ldHorizontal
      Index = 3
    end
    object dxLayoutControlLabeledItem2: TdxLayoutLabeledItem
      Parent = dxLayoutControlGroup8
      AlignHorz = ahCenter
      AlignVert = avCenter
      CaptionOptions.Glyph.Data = {
        36040000424D3604000000000000360000002800000010000000100000000100
        2000000000000004000000000000000000000000000000000000000000040000
        00020000000000000001000000060000000C0000001000000012000000110000
        000D000000070000000200000000000000000000000000000000533D358B0000
        000C000000050000000D31201B68694439C2875748EF925E4EFF855546EF6641
        35C32F1E186B0000001000000004000000010000000000000000AA7D6EFF4F36
        2E920D09082C6A493EBBA87C6DFFD2B6A6FFE7D1C0FFEEDBCAFFE5CFBFFFCFB1
        A1FFA27564FF623F33BE0C07062B000000060000000000000000AF8375FFC7A8
        99FF986B5BF5C8A99BFFEFE2D3FFF3E6DBFFF5ECE2FFF8F0E7FFF5ECE2FFF3E6
        DAFFEEDDCEFFC19F8EFF845647EC0D09072D0000000400000000B3897AFFF2E5
        D8FFE0C8B8FFF0E1D3FFEBDBD2FFCDB2A7FFB38C7EFFA17263FFB28C7EFFCCB2
        A7FFEADCD1FFF0E1D4FFC9AA9AFF633F33BD0000000F00000001B68E7FFFF3E6
        DBFFF0E1D1FFEBD9CAFFBF9C8EFF64463DAE2D1F1B580604041B2C1F1B596346
        3DAEBF9E91FFE8D9CDFFEFDFD1FFA97E6EFF321F1A6B00000006BA9284FFFAF6
        F1FFF9F3EDFFF9F4EDFFCDB3A8FF543C34930000000D00000003000000040403
        031565483EADCFB5AAFFF9F2EBFFD5BEB4FF674337C10000000BBC9587FFBA93
        84FFB88F81FFB58C7EFFB3897AFFB08576FF57413A8800000003000000000000
        000431252155AF8475FFAD8274FFAD8272FF9C7567EA0000000A0000000B0000
        000F000000100000000F0000000C000000090000000500000002000000050000
        000A0000001000000016000000190000001900000014000000069F7569EBAC81
        72FFAC8171FFAC8070FF30231F50000000030000000000000002543E3685AA7D
        6EFFA97D6DFFA97C6DFFA97C6CFFA97B6CFFAE8374FF0000000A7B5D52B8E0CF
        C7FFF5ECE1FFC8AA9DFF5E3F35A60403020F000000020000000200000009523A
        328FCEB4A8FFF5ECDFFFF4E9DDFFF4E9DBFFB3887AFF0000000C3E302A60C3A2
        97FFFAF5EFFFEDDFD3FFB89486FF5E3E34A62A1C1850060403142B1E19526245
        3BA8BF9E91FFF1E6DBFFF6EEE3FFF5EBDFFFB68E7FFF0000000B02020106916F
        63CEDECCC3FFFBF5EFFFE9DCD1FFC7AB9DFFAB8373FF9A6958FFAE8677FFCBAF
        A2FFEDDFD4FFFAF4ECFFEBE0D8FFF9F4ECFFB99183FF0000000A000000011A14
        1228A78477E9D8C2B9FFFAF5F2FFF9F3ECFFF7F1E7FFF7EFE5FFF7F1E7FFF8F2
        EAFFF9F5EFFFD6C0B5FFAB8476F4D7C1B8FFBC9586FF00000009000000000000
        00010F0C0B197D6258B1C3A295FFE6D7D1FFF6F0EAFFFCFAF7FFF5EFE9FFE5D6
        CEFFC3A194FF7C6056B30F0C0A1F59453E87BD9789FF00000007000000000000
        00000000000000000002372B264F6D554D99917266CBB58F7FF9917266CC6C55
        4D9B362A26510000000400000002000000055E4B448300000003}
      CaptionOptions.Text = 'Synchronizing storages...'
      Offsets.Left = 10
      Offsets.Right = 10
      Index = 0
    end
    object dxLayoutControlGroup6: TdxLayoutGroup
      Parent = dxLayoutControlGroupConnect
      AlignHorz = ahCenter
      AlignVert = avCenter
      CaptionOptions.Text = 'New Group'
      CaptionOptions.Visible = False
      ButtonOptions.Buttons = <>
      LayoutDirection = ldHorizontal
      Index = 0
    end
    object dxLayoutControlGroup8: TdxLayoutGroup
      Parent = dxLayoutControlGroupSync
      AlignHorz = ahCenter
      AlignVert = avCenter
      CaptionOptions.Text = 'New Group'
      CaptionOptions.Visible = False
      ButtonOptions.Buttons = <>
      LayoutDirection = ldHorizontal
      Index = 0
    end
  end
  object dxStatusBar: TdxStatusBar
    Left = 0
    Top = 786
    Width = 1184
    Height = 26
    Panels = <
      item
        PanelStyleClassName = 'TdxStatusBarContainerPanelStyle'
        PanelStyle.Container = dxStatusBarContainer5
        Bevel = dxpbNone
        MinWidth = 64
        Width = 64
      end
      item
        PanelStyleClassName = 'TdxStatusBarContainerPanelStyle'
        PanelStyle.Container = dxStatusBarContainer9
        Bevel = dxpbNone
        MinWidth = 76
        Width = 76
      end
      item
        PanelStyleClassName = 'TdxStatusBarTextPanelStyle'
        Bevel = dxpbNone
        Fixed = False
      end
      item
        PanelStyleClassName = 'TdxStatusBarTextPanelStyle'
        PanelStyle.Alignment = taCenter
        PanelStyle.Font.Charset = DEFAULT_CHARSET
        PanelStyle.Font.Color = clGray
        PanelStyle.Font.Height = -12
        PanelStyle.Font.Name = 'Arial'
        PanelStyle.Font.Style = []
        PanelStyle.ParentFont = False
        Text = 'MIT License'
        Width = 90
      end
      item
        PanelStyleClassName = 'TdxStatusBarTextPanelStyle'
        PanelStyle.Alignment = taCenter
        PanelStyle.Font.Charset = DEFAULT_CHARSET
        PanelStyle.Font.Color = clGray
        PanelStyle.Font.Height = -12
        PanelStyle.Font.Name = 'Arial'
        PanelStyle.Font.Style = []
        PanelStyle.ParentFont = False
        Text = #169' Nikita Chisnikov, 2015'
        Width = 156
      end
      item
        PanelStyleClassName = 'TdxStatusBarTextPanelStyle'
        PanelStyle.Alignment = taCenter
        PanelStyle.Font.Charset = DEFAULT_CHARSET
        PanelStyle.Font.Color = clGray
        PanelStyle.Font.Height = -12
        PanelStyle.Font.Name = 'Arial'
        PanelStyle.Font.Style = []
        PanelStyle.ParentFont = False
        Bevel = dxpbNone
        BiDiMode = bdLeftToRight
        ParentBiDiMode = False
        Text = 'github.com/nikchis/smartnote'
        Width = 180
      end
      item
        PanelStyleClassName = 'TdxStatusBarTextPanelStyle'
        Bevel = dxpbNone
        Fixed = False
      end
      item
        PanelStyleClassName = 'TdxStatusBarContainerPanelStyle'
        PanelStyle.Container = dxStatusBarContainer3
        Bevel = dxpbNone
        MinWidth = 34
        Width = 34
      end
      item
        PanelStyleClassName = 'TdxStatusBarContainerPanelStyle'
        PanelStyle.Container = dxStatusBarContainer4
        Bevel = dxpbNone
        MinWidth = 62
        Width = 62
      end>
    PaintStyle = stpsUseLookAndFeel
    SizeGrip = False
    LookAndFeel.NativeStyle = False
    Font.Charset = DEFAULT_CHARSET
    Font.Color = clWindowText
    Font.Height = -13
    Font.Name = '@Arial Unicode MS'
    Font.Style = []
    object dxStatusBarContainer3: TdxStatusBarContainerControl
      Left = 1084
      Top = 1
      Width = 36
      Height = 24
      object cxButtonChangePwd: TcxButton
        Left = 0
        Top = 0
        Width = 36
        Height = 24
        Hint = 'Set new password'
        Align = alClient
        OptionsImage.Glyph.Data = {
          36040000424D3604000000000000360000002800000010000000100000000100
          2000000000000004000000000000000000000000000000000000000000330000
          0033000000330000003300000033000000000000000000000000000000000000
          00000000000000000000000000000000000000000000000000002F76B6FF2E73
          B4FF2F73B3FF3073B4FF3077B6FF000000330000000000000000000000000000
          00000000000000000000000000000000000000000000000000002F75B5FF4FC8
          FCFF4DC3F8FF4DC4F8FF3887C5FF3378B6FF0000000000000000000000000000
          00000000000000000000000000000000000000000000000000002D6BA6E83B92
          CEFF4DC1F6FF4CBFF4FF4DC3F8FF3176B5FF0000003300000033000000000000
          0000000000000000000000000000000000000000000000000000000000002B69
          A0E23E92CCFF53C6F8FF53C5F8FF2F72B0FF3377B5FF357BB9FF000000330000
          0000000000000000000000000000000000000000000000000000000000000000
          00002C69A1E24294CDFF5ECCFCFF5CCBFAFF5ECDFDFF3174B3FF357AB7FF0000
          003300000033000000310000001A000000000000000000000000000000000000
          0000000000002D6AA0E24597CFFF67D2FCFF65CFFAFF67D2FCFF57B2E2FF3578
          B4FF377AB6FF3878B2F91D415F9C0000002E0000000000000000000000000000
          000000000000000000002E69A0E04999CFFF72D5FDFF5FC6F8FF48B2F4FF4EB7
          F7FF4FB9F9FF44ABF0FF3D8CCBFF3674A8EC0000001A00000000000000000000
          00000000000000000000000000003376B4FF6DD1FEFF58BDF6FF75D5FBFF79D8
          FCFF7AD9FCFF77D8FEFF5DC5FCFF3F8FCCFF1E425F9B00000000000000000000
          00000000000000000000000000003578B5FF62CAFEFF81DCFEFF7FDBFDFF80DC
          FFFF81DEFFFF82DFFFFF85E1FFFF5ABFF6FF3C80B9FF00000000000000000000
          00000000000000000000000000003B7FBAFF6FD5FFFF7CDAFDFF7CDAFDFF70CA
          F1FF4C97CBFF4C98CCFF73CEF4FF71D9FFFF3C7EB8FF00000000000000000000
          00000000000000000000000000003A80BCFF74DBFFFF77D8FCFF78DAFEFF4A96
          CBFF060E151D070E151D4D99CEFF77E0FFFF3D7FBAFF00000000000000000000
          0000000000000000000000000000387BB8FF6FD3F8FF73D9FCFF73DAFDFF4A97
          CCFF00000033000000334D9BCFFF73D7FBFF3F82BBFF00000000000000000000
          00000000000000000000000000001F415F824B99CEFF71DBFFFF6FD8FDFF66C8
          F0FF4B99CDFF4D9ACFFF69CEF5FF4F9ED1FF2143608200000000000000000000
          0000000000000000000000000000000000003673A8E64A99CEFF67CEF6FF6EDB
          FFFF6FDDFFFF69D3F9FF4E9ED1FF3A77AAE70000000000000000000000000000
          0000000000000000000000000000000000000000000020435F823E80BAFF3F80
          BAFF3F81BAFF3F82BBFF21436082000000000000000000000000}
        ParentShowHint = False
        ShowHint = True
        TabOrder = 0
        Font.Charset = DEFAULT_CHARSET
        Font.Color = clWindowText
        Font.Height = -12
        Font.Name = 'Arial'
        Font.Style = []
        ParentFont = False
        OnClick = cxButtonChangePwdClick
        ExplicitWidth = 34
      end
    end
    object dxStatusBarContainer4: TdxStatusBarContainerControl
      Left = 1121
      Top = 1
      Width = 64
      Height = 24
      object cxButtonConnect: TcxButton
        Left = 0
        Top = 0
        Width = 64
        Height = 24
        Hint = 'Connection settings'
        Align = alClient
        Caption = 'Settings'
        ParentShowHint = False
        ShowHint = True
        TabOrder = 0
        Font.Charset = DEFAULT_CHARSET
        Font.Color = clWindowText
        Font.Height = -12
        Font.Name = 'Arial'
        Font.Style = []
        ParentFont = False
        OnClick = cxButtonConnectClick
        ExplicitWidth = 62
      end
    end
    object dxStatusBarContainer5: TdxStatusBarContainerControl
      Left = 1
      Top = 1
      Width = 66
      Height = 24
      object cxButtonBackup: TcxButton
        Left = 0
        Top = 0
        Width = 66
        Height = 24
        Hint = 'Save data to remote storage'
        Align = alClient
        Caption = 'Save'
        OptionsImage.Glyph.Data = {
          36040000424D3604000000000000360000002800000010000000100000000100
          2000000000000004000000000000000000000000000000000000000000000000
          0000000000000000000000000000000000000000000000000000000000000000
          0000000000000000000000000000000000000000000000000000000000000000
          0000000000000000000000000000000000000000000000000000000000000000
          0000000000000000000000000000000000000000000000000000000000001E1E
          1E7E00000000000000000707071D1A1A1A702B2B2BB7373737EA373737EA2B2B
          2BB71A1A1A700707071D00000000000000000000000000000000000000003C3C
          3CFF1E1E1E7E0D0D0D39292929B03C3C3CFF3C3C3CFF3C3C3CFF3C3C3CFF3C3C
          3CFF3C3C3CFF292929B00D0D0D39000000000000000000000000000000003C3C
          3CFF3C3C3CFF333333D73C3C3CFF2D2D2DBF181818660707071F0707071F1818
          18662D2D2DBF3C3C3CFF292929B00707071D0000000000000000000000003C3C
          3CFF3C3C3CFF3C3C3CFF343434DF0C0C0C330000000000000000000000000000
          00000C0C0C332D2D2DBF3C3C3CFF1A1A1A700000000000000000000000003C3C
          3CFF3C3C3CFF3C3C3CFF3C3C3CFF1E1E1E7E0000000000000000000000000000
          0000000000000000000000000000000000000000000000000000000000003C3C
          3CFF3C3C3CFF3C3C3CFF3C3C3CFF3C3C3CFF1E1E1E7E00000000000000000000
          0000000000000000000000000000000000000000000000000000000000000000
          0000000000000000000000000000000000000000000000000000000000001E1E
          1E7E3C3C3CFF3C3C3CFF3C3C3CFF3C3C3CFF3C3C3CFF00000000000000000000
          0000000000000000000000000000000000000000000000000000000000000000
          00001E1E1E7E3C3C3CFF3C3C3CFF3C3C3CFF3C3C3CFF00000000000000000000
          00001A1A1A703C3C3CFF2D2D2DBF0C0C0C330000000000000000000000000000
          00000C0C0C33343434DF3C3C3CFF3C3C3CFF3C3C3CFF00000000000000000000
          00000707071D292929B03C3C3CFF2D2D2DBF181818660707071F0707071F1818
          18662D2D2DBF3C3C3CFF333333D73C3C3CFF3C3C3CFF00000000000000000000
          0000000000000D0D0D39292929B03C3C3CFF3C3C3CFF3C3C3CFF3C3C3CFF3C3C
          3CFF3C3C3CFF292929B00D0D0D391E1E1E7E3C3C3CFF00000000000000000000
          000000000000000000000707071D1A1A1A702B2B2BB7373737EA373737EA2B2B
          2BB71A1A1A700707071D00000000000000001E1E1E7E00000000000000000000
          0000000000000000000000000000000000000000000000000000000000000000
          0000000000000000000000000000000000000000000000000000000000000000
          0000000000000000000000000000000000000000000000000000000000000000
          0000000000000000000000000000000000000000000000000000}
        ParentShowHint = False
        ShowHint = True
        TabOrder = 0
        Font.Charset = DEFAULT_CHARSET
        Font.Color = clWindowText
        Font.Height = -12
        Font.Name = 'Arial'
        Font.Style = []
        ParentFont = False
        OnClick = cxButtonBackupClick
        ExplicitWidth = 72
      end
    end
    object dxStatusBarContainer9: TdxStatusBarContainerControl
      Left = 68
      Top = 1
      Width = 78
      Height = 24
      object cxButtonRestore: TcxButton
        Left = 0
        Top = 0
        Width = 78
        Height = 24
        Hint = 'Restore data from remote storage'
        Align = alClient
        Caption = 'Restore'
        OptionsImage.Glyph.Data = {
          36040000424D3604000000000000360000002800000010000000100000000100
          2000000000000004000000000000000000000000000000000000000000000000
          0000000000000000000000000000000000000000000000000000000000000000
          0000000000000000000000000000000000000000000000000000000000000000
          0000000000000000000000000000000000000000000000000000000000000000
          0000000000000000000000000000000000000000000000000000000000000000
          000000000000000000000707071D1A1A1A702B2B2BB7373737EA373737EA2B2B
          2BB71A1A1A700707071D00000000000000000000000000000000000000000000
          0000000000000D0D0D39292929B03C3C3CFF3C3C3CFF3C3C3CFF3C3C3CFF3C3C
          3CFF3C3C3CFF292929B00D0D0D39000000000000000000000000000000000000
          00000707071D292929B03C3C3CFF2D2D2DBF181818660707071F0707071F1818
          18662D2D2DBF3C3C3CFF292929B00707071D0000000000000000000000000000
          00001A1A1A703C3C3CFF2D2D2DBF0C0C0C330000000000000000000000000000
          00000C0C0C332D2D2DBF3C3C3CFF1A1A1A700000000000000000000000000000
          00002B2B2BB73C3C3CFF18181866000000000000000000000000000000000000
          000000000000181818663C3C3CFF2B2B2BB70000000000000000000000000000
          0000000000000000000000000000000000000000000000000000000000000000
          0000000000000707071F3C3C3CFF373737EA0000000000000000000000003C3C
          3CFF3C3C3CFF3C3C3CFF3C3C3CFF3C3C3CFF1E1E1E7E00000000000000000000
          0000000000000707071F3C3C3CFF373737EA0000000000000000000000003C3C
          3CFF3C3C3CFF3C3C3CFF3C3C3CFF1E1E1E7E0000000000000000000000000000
          000000000000181818663C3C3CFF2B2B2BB70000000000000000000000003C3C
          3CFF3C3C3CFF3C3C3CFF343434DF0C0C0C330000000000000000000000000000
          00000C0C0C332D2D2DBF3C3C3CFF1A1A1A700000000000000000000000003C3C
          3CFF3C3C3CFF333333D73C3C3CFF2D2D2DBF181818660707071F0707071F1818
          18662D2D2DBF3C3C3CFF292929B00707071D0000000000000000000000003C3C
          3CFF1E1E1E7E0D0D0D39292929B03C3C3CFF3C3C3CFF3C3C3CFF3C3C3CFF3C3C
          3CFF3C3C3CFF292929B00D0D0D39000000000000000000000000000000001E1E
          1E7E00000000000000000707071D1A1A1A702B2B2BB7373737EA373737EA2B2B
          2BB71A1A1A700707071D00000000000000000000000000000000000000000000
          0000000000000000000000000000000000000000000000000000000000000000
          0000000000000000000000000000000000000000000000000000000000000000
          0000000000000000000000000000000000000000000000000000000000000000
          0000000000000000000000000000000000000000000000000000}
        ParentShowHint = False
        ShowHint = True
        TabOrder = 0
        Font.Charset = DEFAULT_CHARSET
        Font.Color = clWindowText
        Font.Height = -12
        Font.Name = 'Arial'
        Font.Style = []
        ParentFont = False
        OnClick = cxButtonRestoreClick
        ExplicitWidth = 76
      end
    end
  end
  object TimerConnect: TTimer
    Enabled = False
    Interval = 50
    OnTimer = TimerConnectTimer
    Left = 800
    Top = 48
  end
end
