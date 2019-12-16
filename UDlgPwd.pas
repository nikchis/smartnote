unit UDlgPwd;

interface

uses
  Windows, Messages, SysUtils, Classes, Graphics, Controls, Forms, Dialogs,
  StdCtrls, cxControls, cxContainer, cxEdit, cxTextEdit, Menus,
  cxLookAndFeelPainters, cxButtons, cxGraphics, cxLookAndFeels, dxSkinsCore,
  dxSkinDevExpressDarkStyle, dxSkinDevExpressStyle, dxSkinFoggy,
  dxSkinLondonLiquidSky, dxSkinMcSkin, dxSkinOffice2010Blue,
  dxSkinOffice2010Silver, dxSkinOffice2013DarkGray, dxSkinOffice2013White,
  dxSkinSevenClassic, dxSkinSharp, dxSkinSharpPlus, dxSkinsDefaultPainters,
  dxSkinVS2010, dxSkinWhiteprint, dxSkinscxPCPainter, dxLayoutContainer,
  dxLayoutControl, dxLayoutcxEditAdapters, dxLayoutControlAdapters, cxGroupBox,
  cxRadioGroup, dxSkinBlack;

type
  TFormPwd = class(TForm)
    cxTextEditPwd1: TcxTextEdit;
    cxTextEditPwd2: TcxTextEdit;
    cxButtonOK: TcxButton;
    cxButton2: TcxButton;
    dxLayoutControl1Group_Root: TdxLayoutGroup;
    dxLayoutControl1: TdxLayoutControl;
    dxLayoutControl1Item1: TdxLayoutItem;
    dxLayoutControl1Item2: TdxLayoutItem;
    dxLayoutControl1Item3: TdxLayoutItem;
    dxLayoutControl1Item4: TdxLayoutItem;
    dxLayoutControl1Group1: TdxLayoutGroup;
    procedure cxTextEditPwd1PropertiesChange(Sender: TObject);
  private
    { Private declarations }
    function GetPwd :String;
    procedure SetPwd(sPwd :String);
  public
    { Public declarations }
    property Pwd :String read GetPwd write SetPwd;
  end;

var
  FormPwd: TFormPwd;

implementation

{$R *.DFM}

uses UDMMain;

procedure TFormPwd.cxTextEditPwd1PropertiesChange(Sender: TObject);
begin
  cxButtonOK.Enabled := (Length(cxTextEditPwd1.Text) >= 6) and
    (Length(cxTextEditPwd2.Text) >= 6) and
    (cxTextEditPwd1.Text = cxTextEditPwd2.Text);
end;

function TFormPwd.GetPwd :String;
begin
  Result := cxTextEditPwd1.Text;
end;

procedure TFormPwd.SetPwd(sPwd :String);
begin
  cxTextEditPwd1.Text := sPwd;
  cxTextEditPwd2.Text := sPwd;
  cxTextEditPwd1PropertiesChange(cxTextEditPwd1);
end;

end.
