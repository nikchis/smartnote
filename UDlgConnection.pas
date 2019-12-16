unit UDlgConnection;

interface

uses
  Winapi.Windows, Winapi.Messages, System.SysUtils, System.Variants, System.Classes, Vcl.Graphics,
  Vcl.Controls, Vcl.Forms, Vcl.Dialogs, cxGraphics, cxControls, cxLookAndFeels,
  cxLookAndFeelPainters, dxSkinsCore, dxSkinsDefaultPainters,
  dxSkinscxPCPainter, dxLayoutContainer, dxLayoutControl,
  dxLayoutcxEditAdapters, dxLayoutControlAdapters, cxContainer, cxEdit,
  Vcl.Menus, Vcl.StdCtrls, cxButtons, cxTextEdit, Data.DB, MemDS, DBAccess,
  LiteAccess, cxCheckBox, dxSkinDevExpressDarkStyle, dxSkinDevExpressStyle,
  dxSkinFoggy, dxSkinLondonLiquidSky, dxSkinMcSkin, dxSkinOffice2010Blue,
  dxSkinOffice2010Silver, dxSkinOffice2013DarkGray, dxSkinOffice2013White,
  dxSkinSevenClassic, dxSkinSharp, dxSkinSharpPlus, dxSkinVS2010,
  dxSkinWhiteprint, cxStyles, cxCustomData, cxFilter, cxData, cxDataStorage,
  cxNavigator, cxDBData, cxGridLevel, cxClasses, cxGridCustomView,
  cxGridCustomTableView, cxGridTableView, cxGridDBTableView, cxGrid,
  cxButtonEdit, cxLabel, dxSkinBlack, ScBridge, ScSSHUtil, ScSSHChannel,
  ScSSHClient, UniProvider, MySQLUniProvider, Uni;

type
  TFormConnection = class(TForm)
    dxLayoutControlGroup_Root: TdxLayoutGroup;
    dxLayoutControl: TdxLayoutControl;
    dxLayoutControlGroup1: TdxLayoutGroup;
    dxLayoutControlGroup2: TdxLayoutGroup;
    dxLayoutControlItem1: TdxLayoutItem;
    dxLayoutControlItem2: TdxLayoutItem;
    dxLayoutControlItem3: TdxLayoutItem;
    dxLayoutControlItem4: TdxLayoutItem;
    dxLayoutControlItem5: TdxLayoutItem;
    dxLayoutControlItem6: TdxLayoutItem;
    dxLayoutControlItem7: TdxLayoutItem;
    dxLayoutControlGroup3: TdxLayoutGroup;
    dxLayoutControlItem8: TdxLayoutItem;
    dxLayoutControlItem9: TdxLayoutItem;
    cxTextEditSshHost: TcxTextEdit;
    cxTextEditSshPort: TcxTextEdit;
    cxTextEditSshLogin: TcxTextEdit;
    cxTextEditSshPwd: TcxTextEdit;
    cxTextEditBaseHost: TcxTextEdit;
    cxTextEditBasePort: TcxTextEdit;
    cxTextEditBaseLogin: TcxTextEdit;
    cxButtonOk: TcxButton;
    cxButtonCancel: TcxButton;
    dxLayoutControlItem10: TdxLayoutItem;
    cxTextEditBasePwd: TcxTextEdit;
    cxTextEditBaseName: TcxTextEdit;
    dxLayoutControlItem13: TdxLayoutItem;
    dxLayoutControlGroup6: TdxLayoutGroup;
    dxLayoutControlGroup4: TdxLayoutGroup;
    dxLayoutControlItem12: TdxLayoutItem;
    dxLayoutControlItem14: TdxLayoutItem;
    dxLayoutControlGroup5: TdxLayoutGroup;
    cxButtonGenKey: TcxButton;
    cxButtonCopyPub: TcxButton;
    cxButtonImportKey: TcxButton;
    dxLayoutControlItem15: TdxLayoutItem;
    OpenDialog: TOpenDialog;
    SaveDialog: TSaveDialog;
    dxLayoutControlGroup7: TdxLayoutGroup;
    dxLayoutControlGroup8: TdxLayoutGroup;
    cxButtonTest: TcxButton;
    dxLayoutControlItem11: TdxLayoutItem;
    UCTest: TUniConnection;
    MySQLUniProvider: TMySQLUniProvider;
    ScSSHClientTest: TScSSHClient;
    ScSSHChannelTest: TScSSHChannel;
    dxLayoutControlItem16: TdxLayoutItem;
    cxTextEditSalt: TcxTextEdit;
    dxLayoutControlGroup9: TdxLayoutGroup;
    procedure cxButtonOkClick(Sender: TObject);
    procedure FormShow(Sender: TObject);
    procedure cxButtonCancelClick(Sender: TObject);
    procedure cxButtonGenKeyClick(Sender: TObject);
    procedure cxButtonCopyPubClick(Sender: TObject);
    procedure cxButtonImportKeyClick(Sender: TObject);
    procedure FormCreate(Sender: TObject);
    procedure cxButtonTestClick(Sender: TObject);
    procedure ScSSHClientTestServerKeyValidate(Sender: TObject;
      NewServerKey: TScKey; var Accept: Boolean);
    procedure dxLayoutControlGroup9Expanded(Sender: TObject);
    procedure dxLayoutControlGroup9Collapsed(Sender: TObject);
  private
    { Private declarations }
  public
    { Public declarations }
  end;

var
  FormConnection: TFormConnection;

implementation

{$R *.dfm}

uses UDMMain;

procedure TFormConnection.cxButtonCancelClick(Sender: TObject);
begin
  cxTextEditSshHost.Text:='';
  cxTextEditSshPort.Text:='';
  cxTextEditSshLogin.Text:='';
  cxTextEditSshPwd.Text:='';
  cxTextEditBaseName.Text:='';
  cxTextEditBaseHost.Text:='';
  cxTextEditBasePort.Text:='';
  cxTextEditBaseLogin.Text:='';
  cxTextEditBasePwd.Text:='';
end;

procedure TFormConnection.cxButtonCopyPubClick(Sender: TObject);
var
  ScKey: TScKey;
begin
  with DMMain.ScRegStorage.Keys do
  begin
    if FindKey(c_priv_key_name)<>nil then
    begin
      SaveDialog.InitialDir := ExtractFilePath(Application.ExeName);
      SaveDialog.FileName := c_priv_key_name;
      if NOT SaveDialog.Execute() then exit;
      try
        ScKey := KeyByName(c_priv_key_name);
        ScKey.Ready := true;
        ScKey.ExportTo(SaveDialog.FileName, true,'');
        ShowInfo('File was succesfully saved.');
      except
        on E: Exception do
        begin
          ShowError(E.Message);
        end;
      end;
    end
    else
    begin
      ShowError('Key does not exist.');
    end;
  end;
end;

procedure TFormConnection.cxButtonGenKeyClick(Sender: TObject);
var
  NewScKey: TScKey;
begin
  if (DMMain.ScRegStorage.Keys.FindKey(c_priv_key_name)<>nil) AND
  not AskQuestion('Are you sure you want to generate new key?'+#13+
  'Current key will be erased!') then exit;
  try
    if DMMain.ScRegStorage.Keys.FindKey(c_priv_key_name)<>nil then
    begin
      DMMain.ScRegStorage.Keys.Remove(DMMain.ScRegStorage.Keys.KeyByName(c_priv_key_name));
    end;
    NewScKey := TScKey.Create(DMMain.ScRegStorage.Keys);
    NewScKey.KeyName := c_priv_key_name;
    NewScKey.Generate(ScSSHUtil.aaRSA, 2048);
    ShowInfo('New key succesfully generated.');
  except
    on E:Exception do
    begin
      ShowError(E.Message); exit;
    end;
  end;
  NewScKey.Destroy;
end;

procedure TFormConnection.cxButtonImportKeyClick(Sender: TObject);
var
  NewScKey: TScKey;
begin
  if NOT OpenDialog.Execute() then exit;
  try
    if DMMain.ScRegStorage.Keys.FindKey(c_priv_key_name)<>nil then
    begin
      DMMain.ScRegStorage.Keys.Remove(DMMain.ScRegStorage.Keys.KeyByName(c_priv_key_name));
    end;
    NewScKey := TScKey.Create(DMMain.ScRegStorage.Keys);
    NewScKey.KeyName := c_priv_key_name;
    NewScKey.ImportFrom(OpenDialog.FileName);
    ShowInfo('New key succesfully imported.');
  except
    on E:Exception do
    begin
      ShowError(E.Message); exit;
    end;
  end;
  NewScKey.Destroy;
end;

procedure TFormConnection.cxButtonOkClick(Sender: TObject);
begin
  with DMMain do
  begin
    try
      if LQNetwork.Active AND (not LQNetwork.IsEmpty) then
      begin
          LQNetwork.Edit;
          LQNetwork.FieldByName('ssh_host').AsString:= cxTextEditSshHost.Text;
          LQNetwork.FieldByName('ssh_port').AsInteger:= StrToInt(cxTextEditSshPort.Text);
          LQNetwork.FieldByName('ssh_user').AsString:= cxTextEditSshLogin.Text;
          LQNetwork.FieldByName('ssh_pwd').AsString:= cxTextEditSshPwd.Text;
          LQNetwork.FieldByName('db_name').AsString:= cxTextEditBaseName.Text;
          LQNetwork.FieldByName('db_fw_host').AsString:= cxTextEditBaseHost.Text;
          LQNetwork.FieldByName('db_fw_port').AsInteger:= StrToInt(cxTextEditBasePort.Text);
          LQNetwork.FieldByName('db_user').AsString:= cxTextEditBaseLogin.Text;
          LQNetwork.FieldByName('db_pwd').AsString:= cxTextEditBasePwd.Text;
          LQNetwork.FieldByName('salt').AsString:= cxTextEditSalt.Text;
          LQNetwork.Post;
          ModalResult:= mrOk;
      end;
    except
      on E:Exception do
      begin
        ShowError(E.Message); exit;
      end;
    end;
  end;
end;

procedure TFormConnection.cxButtonTestClick(Sender: TObject);
begin
  UCTest.Disconnect;
  ScSSHChannelTest.Disconnect;
  ScSSHClientTest.Disconnect;
  if (Length(cxTextEditSshHost.Text)>0) AND
  (Length(cxTextEditSshLogin.Text)>0) AND
  (Length(cxTextEditSshPort.Text)>0) then
  begin
    ScSSHClientTest.HostName:= cxTextEditSshHost.Text;
    ScSSHClientTest.User:= cxTextEditSshLogin.Text;
    ScSSHClientTest.Password:= cxTextEditSshPwd.Text;
    ScSSHClientTest.Port:=  StrToInt(cxTextEditSshPort.Text);
    if DMMain.ScRegStorage.Keys.FindKey(c_priv_key_name) <> nil then
    begin
      ScSSHClientTest.Authentication := ScSSHUtil.atPublicKey;
      ScSSHClientTest.PrivateKeyName := c_priv_key_name;
      try
        ScSSHClientTest.Connect;
      except
        on E: Exception do
        begin
            ScSSHClientTest.Authentication := ScSSHUtil.atPassword;
            try
              ScSSHClientTest.Connect;
            except
              on E: Exception do
              begin
                ShowError('SSH connection error.'+#13+E.Message);
                Exit;
              end;
            end;
        end;
      end;
    end
    else
    begin
      ScSSHClientTest.Authentication := ScSSHUtil.atPassword;
      try
        ScSSHClientTest.Connect;
      except
        on E: Exception do
        begin
          ShowError('SSH connection error.'+#13+E.Message);
          Exit;
        end;
      end;
    end;
  end;
  if (Length(cxTextEditBaseHost.Text)>0) AND
  (Length(cxTextEditBasePort.Text)>0) then
  begin
    if ScSSHClientTest.Connected then
    begin
      UCTest.Server:= '127.0.0.1';
      UCTest.Port:= 3308;
      ScSSHChannelTest.SourcePort:= 3308;
      ScSSHChannelTest.DestPort:=  StrToInt(cxTextEditBasePort.Text);
      ScSSHChannelTest.DestHost:= cxTextEditBaseHost.Text;
      try
        ScSSHChannelTest.Connect;
      except
        on E: Exception do
        begin
          ScSSHChannelTest.Disconnect;
          ScSSHClientTest.Disconnect;
          ShowError('SSH port forwarding error.'+#13+'(local port 3308)'
          +#13+E.Message);
          Exit;
        end;
      end;
    end
    else
    begin
      UCTest.Server:= cxTextEditBaseHost.Text;
      UCTest.Port:= StrToInt(cxTextEditBasePort.Text);
    end;
  end
  else
  begin
    ScSSHClientTest.Disconnect;
    ShowError('MySQL host or port is null.');
    Exit;
  end;
  if (Length(cxTextEditBaseName.Text)>0) AND
  (Length(cxTextEditBaseLogin.Text)>0) then
  begin
    UCTest.Database:= cxTextEditBaseName.Text;
    UCTest.Username:= cxTextEditBaseLogin.Text;
    UCTest.Password:= cxTextEditBasePwd.Text;
    try
      UCTest.Connect;
      ShowInfo('Connection successfully established.');
      UCTest.Disconnect;
      ScSSHChannelTest.Disconnect;
      ScSSHClientTest.Disconnect;
    except
      on E: Exception do
      begin
        UCTest.Disconnect;
        ScSSHChannelTest.Disconnect;
        ScSSHClientTest.Disconnect;
        ShowError('MySQL connection error.'+#13+E.Message);
        Exit;
      end;
    end;
  end
  else
  begin
    ScSSHChannelTest.Disconnect;
    ScSSHClientTest.Disconnect;
    ShowError('MySQL DB or user is null.');
  end;
end;

procedure TFormConnection.dxLayoutControlGroup9Collapsed(Sender: TObject);
begin
  FormConnection.Height:= 366;
end;

procedure TFormConnection.dxLayoutControlGroup9Expanded(Sender: TObject);
begin
  FormConnection.Height:= 418;
end;

procedure TFormConnection.FormCreate(Sender: TObject);
begin
  OpenDialog.InitialDir:= ExtractFilePath(Application.ExeName);
end;

procedure TFormConnection.FormShow(Sender: TObject);
begin
  with DMMain do
  begin
    if LQNetwork.Active AND (not LQNetwork.IsEmpty) then
    begin
      cxTextEditSshHost.Text:=LQNetwork.FieldByName('ssh_host').AsString;
      cxTextEditSshPort.Text:=LQNetwork.FieldByName('ssh_port').AsString;
      cxTextEditSshLogin.Text:=LQNetwork.FieldByName('ssh_user').AsString;
      cxTextEditSshPwd.Text:=LQNetwork.FieldByName('ssh_pwd').AsString;
      cxTextEditBaseName.Text:=LQNetwork.FieldByName('db_name').AsString;
      cxTextEditBaseHost.Text:=LQNetwork.FieldByName('db_fw_host').AsString;
      cxTextEditBasePort.Text:=LQNetwork.FieldByName('db_fw_port').AsString;
      cxTextEditBaseLogin.Text:=LQNetwork.FieldByName('db_user').AsString;
      cxTextEditBasePwd.Text:=LQNetwork.FieldByName('db_pwd').AsString;
      cxTextEditSalt.Text:=LQNetwork.FieldByName('salt').AsString;
    end;
  end;
end;

procedure TFormConnection.ScSSHClientTestServerKeyValidate(Sender: TObject;
  NewServerKey: TScKey; var Accept: Boolean);
begin
  Accept:=true;
end;

end.
