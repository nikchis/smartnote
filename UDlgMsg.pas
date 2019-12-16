unit UDlgMsg;

interface

uses
  Winapi.Windows, Winapi.Messages, System.SysUtils, System.Variants, System.Classes, Vcl.Graphics,
  Vcl.Controls, Vcl.Forms, Vcl.Dialogs, cxGraphics, cxControls, cxLookAndFeels,
  cxLookAndFeelPainters, dxSkinsCore, dxSkinOffice2010Silver,
  dxSkinscxPCPainter, dxLayoutContainer, dxLayoutControl,
  dxLayoutControlAdapters, dxLayoutcxEditAdapters, Vcl.Menus, cxContainer,
  cxEdit, cxTextEdit, cxMemo, Vcl.StdCtrls, cxButtons, dxGDIPlusClasses,
  Vcl.ExtCtrls, dxSkinDevExpressDarkStyle, dxSkinDevExpressStyle, dxSkinFoggy,
  dxSkinLondonLiquidSky, dxSkinMcSkin, dxSkinOffice2010Blue,
  dxSkinOffice2013DarkGray, dxSkinOffice2013White, dxSkinSevenClassic,
  dxSkinSharp, dxSkinSharpPlus, dxSkinsDefaultPainters, dxSkinVS2010,
  dxSkinWhiteprint, dxSkinBlack;

type
  TFormMsg = class(TForm)
    dxLayoutControlGroup_Root: TdxLayoutGroup;
    dxLayoutControl: TdxLayoutControl;
    ImageInfo: TImage;
    dxLayoutControlItemInfo: TdxLayoutItem;
    ImageError: TImage;
    dxLayoutControlItemError: TdxLayoutItem;
    dxLayoutControlItemOk: TdxLayoutItem;
    cxButton1: TcxButton;
    dxLayoutControlItem4: TdxLayoutItem;
    dxLayoutControlGroup4: TdxLayoutGroup;
    dxLayoutControlGroupTab: TdxLayoutGroup;
    cxMemo: TcxMemo;
    cxButtonYes: TcxButton;
    cxButtonNo: TcxButton;
    dxLayoutControlGroupYesNo: TdxLayoutGroup;
    dxLayoutControlItem1: TdxLayoutItem;
    dxLayoutControlItem3: TdxLayoutItem;
    dxLayoutControlItemQuestion: TdxLayoutItem;
    ImageQuestion: TImage;
    procedure cxButtonYesClick(Sender: TObject);
    procedure FormShow(Sender: TObject);
  private
    _yes:boolean;
    { Private declarations }
  public
    { Public declarations }
    property Yes:boolean read _yes;
  end;

var
  FormMsg: TFormMsg;

implementation

{$R *.dfm}

uses UDMMain;

procedure TFormMsg.cxButtonYesClick(Sender: TObject);
begin
  _yes:=true;
end;

procedure TFormMsg.FormShow(Sender: TObject);
begin
  Update;
  _yes:=false;
end;

end.
