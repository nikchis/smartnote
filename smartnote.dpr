program smartnote;

uses
  Vcl.Forms,
  UDlgMsg in 'UDlgMsg.pas' {FormMsg},
  UDlgPwd in 'UDlgPwd.pas' {FormPwd},
  UDMEnc in 'UDMEnc.pas' {DMEnc: TDataModule},
  UDMMain in 'UDMMain.pas' {DMMain: TDataModule},
  UMain in 'UMain.pas' {FormMain},
  UDlgHistory in 'UDlgHistory.pas' {FormHistory},
  UDlgConnection in 'UDlgConnection.pas' {FormConnection};

{$R *.res}

begin
  Application.Initialize;
  Application.MainFormOnTaskbar := True;
  Application.Title := 'Smartnote';
  Application.CreateForm(TFormMain, FormMain);
  Application.CreateForm(TFormMsg, FormMsg);
  Application.CreateForm(TFormPwd, FormPwd);
  Application.CreateForm(TDMEnc, DMEnc);
  Application.CreateForm(TDMMain, DMMain);
  Application.CreateForm(TFormHistory, FormHistory);
  Application.CreateForm(TFormConnection, FormConnection);
  Application.Run;
end.
