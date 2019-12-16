unit UDlgHistory;

interface

uses
  Winapi.Windows, Winapi.Messages, System.SysUtils, System.Variants, System.Classes, Vcl.Graphics,
  Vcl.Controls, Vcl.Forms, Vcl.Dialogs, cxGraphics, cxControls, cxLookAndFeels,
  cxLookAndFeelPainters, dxSkinsCore, dxSkinDevExpressDarkStyle,
  dxSkinDevExpressStyle, dxSkinFoggy, dxSkinLondonLiquidSky, dxSkinMcSkin,
  dxSkinOffice2010Blue, dxSkinOffice2010Silver, dxSkinOffice2013DarkGray,
  dxSkinOffice2013White, dxSkinSevenClassic, dxSkinSharp, dxSkinSharpPlus,
  dxSkinsDefaultPainters, dxSkinVS2010, dxSkinWhiteprint, dxSkinscxPCPainter,
  dxLayoutContainer, dxLayoutControl, Data.DB, DBAccess, LiteAccess, MemDS,
  cxStyles, cxCustomData, cxFilter, cxData, cxDataStorage, cxEdit, cxNavigator,
  cxDBData, cxGridLevel, cxClasses, cxGridCustomView, cxGridCustomTableView,
  cxGridTableView, cxGridDBTableView, cxGrid, cxCalendar, cxButtonEdit,
  dxSkinBlack;

type
  TFormHistory = class(TForm)
    dxLayoutControlGroup_Root: TdxLayoutGroup;
    dxLayoutControl: TdxLayoutControl;
    LQNoteHsts: TLiteQuery;
    LDSNoteHsts: TLiteDataSource;
    cxGridNotes: TcxGrid;
    cxGridNotesDBTableView1: TcxGridDBTableView;
    cxGridNotesLevel1: TcxGridLevel;
    dxLayoutControlItem1: TdxLayoutItem;
    LQNoteHstsnote_hst_id: TIntegerField;
    LQNoteHstscreated: TDateTimeField;
    cxGridNotesDBTableView1note_hst_id: TcxGridDBColumn;
    cxGridNotesDBTableView1created: TcxGridDBColumn;
    cxGridNotesDBTableView1Column1: TcxGridDBColumn;
    LQNoteHstsinfo: TMemoField;
    procedure FormShow(Sender: TObject);
    procedure FormClose(Sender: TObject; var Action: TCloseAction);
    procedure cxGridNotesDBTableView1Column1PropertiesButtonClick(
      Sender: TObject; AButtonIndex: Integer);
  private
    _note_id:integer;
    _info: string;
    { Private declarations }
  public
    property NoteId:integer write _note_id;
    property Info:string read _info;
    { Public declarations }
  end;

var
  FormHistory: TFormHistory;

implementation

{$R *.dfm}

uses UDMMain;

procedure TFormHistory.cxGridNotesDBTableView1Column1PropertiesButtonClick(
  Sender: TObject; AButtonIndex: Integer);
begin
  _info:= LQNoteHsts.FieldByName('info').AsString;
  ModalResult:= mrOk;
end;

procedure TFormHistory.FormClose(Sender: TObject; var Action: TCloseAction);
begin
  LQNoteHsts.Close;
end;

procedure TFormHistory.FormShow(Sender: TObject);
begin
  LQNoteHsts.ParamByName('note_id').AsInteger:= _note_id;
  LQNoteHsts.Open;
end;

end.
