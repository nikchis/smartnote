object FormHistory: TFormHistory
  Left = 0
  Top = 0
  BorderIcons = [biSystemMenu]
  Caption = 'History'
  ClientHeight = 362
  ClientWidth = 264
  Color = clBtnFace
  Font.Charset = DEFAULT_CHARSET
  Font.Color = clWindowText
  Font.Height = -15
  Font.Name = 'Arial'
  Font.Style = []
  OldCreateOrder = False
  Position = poScreenCenter
  OnClose = FormClose
  OnShow = FormShow
  PixelsPerInch = 96
  TextHeight = 17
  object dxLayoutControl: TdxLayoutControl
    Left = 0
    Top = 0
    Width = 264
    Height = 362
    Align = alClient
    TabOrder = 0
    LayoutLookAndFeel = DMMain.dxLayoutSkinLookAndFeel
    ExplicitWidth = 234
    object cxGridNotes: TcxGrid
      Left = 13
      Top = 13
      Width = 221
      Height = 478
      TabOrder = 0
      object cxGridNotesDBTableView1: TcxGridDBTableView
        Navigator.Buttons.CustomButtons = <>
        DataController.DataSource = LDSNoteHsts
        DataController.KeyFieldNames = 'note_hst_id'
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
        OptionsData.Inserting = False
        OptionsView.ColumnAutoWidth = True
        OptionsView.GroupByBox = False
        object cxGridNotesDBTableView1note_hst_id: TcxGridDBColumn
          DataBinding.FieldName = 'note_hst_id'
          Visible = False
        end
        object cxGridNotesDBTableView1created: TcxGridDBColumn
          Caption = 'Date'
          DataBinding.FieldName = 'created'
          PropertiesClassName = 'TcxDateEditProperties'
          Properties.Alignment.Horz = taCenter
          Options.Editing = False
          Width = 150
        end
        object cxGridNotesDBTableView1Column1: TcxGridDBColumn
          PropertiesClassName = 'TcxButtonEditProperties'
          Properties.Buttons = <
            item
              Default = True
              Glyph.Data = {
                36040000424D3604000000000000360000002800000010000000100000000100
                2000000000000004000000000000000000000000000000000000000000000000
                0000000000000000000000000000000000000000000000000000000000000000
                0000000000000000000000000000000000000000000000000000000000000000
                0000000000000000000000000000000000000000000000000000000000000000
                0000000000000000000000000000000000000000000000000000000000000000
                0000000000000000000000000000000000000000000000000000000000000000
                0000000000000000000000000000000000000000000000000000000000000000
                0000000000000000000000000000000000000000000000000000000000000000
                0000000000000000000000000000000000000000000000000000000000300000
                0033000000330000003300000033000000330000000000000000000000000000
                0000000000000000000000000000000000000000000000000000AE7C40F2B782
                41FFB68242FFB78242FFB88342FFBA8545FF0000000000000000000000000000
                0000000000000000000000000000000000000000002400000031B78240FFFDE6
                C3FFECBA6DFFEDBC70FFF2D9B5FF785933AF0000000000000000000000000000
                000000000000000000000000000000000021825E31C1B68344F9B6803EFFF4DB
                B5FFDDA859FFDFAB5EFFC7934CFF7E592EBF00000030000000150000000A0000
                000A0000000A00000015000000317B582EB9BB8747FFBB8747FFB6803EFFF9ED
                D9FFEFD9B8FFDCAE6CFFD7A050FFC48E47FFAE7A3EF44C361B8623190D59241A
                0D5B23190D594C361B87B27E3FF6C29357FFBB8646FF34261447B88241FFDFC0
                91FFC69454FFEDDABCFFE6CBA3FFDCB47BFFCFA060FFC6995FFFC4965EFFC597
                5EFFC5975FFFCEA571FFE2C7A0FFC5975DFF7C592EAA00000000BA8545FF7654
                2AA37351289FBD8B4EFFD9B98FFFE9D4B7FFECDBBFFFEDDBBFFFEDDBBFFFEDDC
                C0FFE7D1B0FFD3B081FFBC8948FF6D4E28960000000000000000000000000000
                000000000000251B0E3379562BA8B07C3DF5B7813EFFB7803EFFB7813EFFB781
                3EFFB8823FFF7A572BA80705030A000000000000000000000000000000000000
                0000000000000000000000000000000000000000000000000000000000000000
                0000000000000000000000000000000000000000000000000000000000000000
                0000000000000000000000000000000000000000000000000000000000000000
                0000000000000000000000000000000000000000000000000000000000000000
                0000000000000000000000000000000000000000000000000000000000000000
                0000000000000000000000000000000000000000000000000000000000000000
                0000000000000000000000000000000000000000000000000000000000000000
                0000000000000000000000000000000000000000000000000000}
              Kind = bkGlyph
            end>
          Properties.ViewStyle = vsButtonsOnly
          Properties.OnButtonClick = cxGridNotesDBTableView1Column1PropertiesButtonClick
          Options.ShowEditButtons = isebAlways
          Width = 45
        end
      end
      object cxGridNotesLevel1: TcxGridLevel
        GridView = cxGridNotesDBTableView1
      end
    end
    object dxLayoutControlGroup_Root: TdxLayoutGroup
      AlignHorz = ahClient
      AlignVert = avClient
      ButtonOptions.Buttons = <>
      Hidden = True
      ShowBorder = False
      Index = -1
    end
    object dxLayoutControlItem1: TdxLayoutItem
      Parent = dxLayoutControlGroup_Root
      AlignHorz = ahClient
      AlignVert = avClient
      CaptionOptions.Text = 'New Item'
      CaptionOptions.Visible = False
      Control = cxGridNotes
      ControlOptions.ShowBorder = False
      Index = 0
    end
  end
  object LQNoteHsts: TLiteQuery
    Connection = DMMain.LCLocal
    SQL.Strings = (
      'SELECT note_hst_id, created, info'
      'FROM note_hsts'
      'WHERE note_id = :note_id'
      'ORDER BY created DESC')
    Left = 72
    Top = 48
    ParamData = <
      item
        DataType = ftUnknown
        Name = 'note_id'
        Value = nil
      end>
    object LQNoteHstsnote_hst_id: TIntegerField
      FieldName = 'note_hst_id'
      Required = True
    end
    object LQNoteHstscreated: TDateTimeField
      FieldName = 'created'
      Required = True
    end
    object LQNoteHstsinfo: TMemoField
      FieldName = 'info'
      BlobType = ftMemo
    end
  end
  object LDSNoteHsts: TLiteDataSource
    DataSet = LQNoteHsts
    Left = 72
    Top = 96
  end
end
