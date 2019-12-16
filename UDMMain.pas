unit UDMMain;

interface

uses
  System.SysUtils, System.Classes, dxSkinsCore, dxSkinDevExpressDarkStyle,
  dxSkinDevExpressStyle, dxSkinFoggy, dxSkinLondonLiquidSky, dxSkinMcSkin,
  dxSkinOffice2010Blue, dxSkinOffice2010Silver, dxSkinOffice2013DarkGray,
  dxSkinOffice2013White, dxSkinSevenClassic, dxSkinSharp, dxSkinSharpPlus,
  dxSkinsDefaultPainters, dxSkinVS2010, dxSkinWhiteprint, cxClasses,
  cxLookAndFeels, dxSkinsForm, dxLayoutLookAndFeels,
  LiteCall, Data.DB, DBAccess, LiteAccess, Forms, MemDS, UniProvider,
  MySQLUniProvider, Uni, ScSSHChannel, ScSSHClient, ScBridge, ScSSHUtil,
  PostgreSQLUniProvider, cxEdit, dxSkinBlack, DADump, LiteDump;

type
  TDMMain = class(TDataModule)
    dxSkinController: TdxSkinController;
    dxLayoutLookAndFeelList: TdxLayoutLookAndFeelList;
    dxLayoutSkinLookAndFeel: TdxLayoutSkinLookAndFeel;
    LCLocal: TLiteConnection;
    ScSSHClient: TScSSHClient;
    ScSSHChannel: TScSSHChannel;
    UCMain: TUniConnection;
    ScRegStorage: TScRegStorage;
    cxEditRepository: TcxEditRepository;
    LQNotes: TLiteQuery;
    LQNotesnote_id: TIntegerField;
    LQNotesupdated: TDateTimeField;
    LQNotesname: TStringField;
    LQNotesinfo_hash: TStringField;
    LDSNotes: TLiteDataSource;
    LQNoteInsert: TLiteQuery;
    LQNoteId: TLiteQuery;
    LQNoteIdnote_id: TIntegerField;
    LQNoteUpdate: TLiteQuery;
    LQNoteDelete: TLiteQuery;
    LQNoteHstInsert: TLiteQuery;
    LQNoteHstId: TLiteQuery;
    LQNoteHstIdnote_hst_id: TIntegerField;
    MySQLUniProvider: TMySQLUniProvider;
    UQNotes: TUniQuery;
    UQNotesnote_id: TIntegerField;
    UQNotesupdated: TDateTimeField;
    UQNotesname: TWideStringField;
    UQNotesinfo_hash: TWideStringField;
    UQNoteUpdate: TUniQuery;
    UQNoteDelete: TUniQuery;
    IntegerField2: TIntegerField;
    DateTimeField2: TDateTimeField;
    WideStringField4: TWideStringField;
    WideStringField5: TWideStringField;
    WideStringField6: TWideStringField;
    UQNoteInsert: TUniQuery;
    IntegerField1: TIntegerField;
    DateTimeField1: TDateTimeField;
    WideStringField1: TWideStringField;
    WideStringField2: TWideStringField;
    WideStringField3: TWideStringField;
    UQNotesinfo: TWideMemoField;
    LQNetwork: TLiteQuery;
    LQNetworknetwork_id: TSmallintField;
    LQNetworkactive: TBooleanField;
    LQNetworkssh_host: TStringField;
    LQNetworkssh_port: TIntegerField;
    LQNetworkssh_user: TStringField;
    LQNetworkssh_pwd: TStringField;
    LQNetworkdb_lc_port: TIntegerField;
    LQNetworkdb_fw_host: TStringField;
    LQNetworkdb_fw_port: TIntegerField;
    LQNetworkdb_name: TStringField;
    LQNetworkdb_user: TStringField;
    LQNetworkdb_pwd: TStringField;
    LQNotessq_note_id: TIntegerField;
    UniSQLNotes: TUniSQL;
    LQNotesinfo: TMemoField;
    LDLocal: TLiteDump;
    LQNetworksalt: TStringField;
    UQCurTs: TUniQuery;
    UQLastSync: TUniQuery;
    UQLastSyncUpdate: TUniQuery;
    UQCurTsts: TLargeintField;
    UQLastSyncts: TIntegerField;
    LQNetworkts: TIntegerField;
    procedure ScSSHClientServerKeyValidate(Sender: TObject;
      NewServerKey: TScKey; var Accept: Boolean);
  private
    { Private declarations }
  public
    function ConnectLocal(Pwd:string):boolean;
    function ConnectRemote():boolean;
    function NeedToSync():boolean;
    function SyncWithRemote():boolean;
    function SyncWithLocal():boolean;
    function CreateNotesTable():boolean;
    { Public declarations }
  end;

const
  c_priv_key_name =     'smartnote_key';
  c_local_storage =     'local.db3';

  mdlg_inf_changes =    'Changes were successfully saved.';
  mdlg_inf_rec_add =    'Record was successfully added.';
  mdlg_inf_recs_add =   'Records were successfully added.';
  mdlg_inf_req_add =    'Request was successfully added.';
  mdlg_inf_lst_add =    'List was successfully added.';
  mdlg_inf_user_reg =   'User registered.';
  mdlg_inf_user_exst =  'User already exists.';
  mdlg_inf_export =     'Export was successfull. File has been saved.';
  mdlg_inf_prs_cnf =    'Records were successfully parsed.';

  mdlg_err_changes =    'Changes were not saved!';
  mdlg_err_connect_db = 'Can not connect to database server!';
  mdlg_err_app_close =  'Application will be closed!';
  mdlg_err_ins_err =    'Error occured during inserting process!';
  mdlg_err_prs_err =    'Error occured during parsing process!';
  mdlg_err_enc_err =    'Error occured during record encryption!';

  c_int32_max_val =     2147483647;
  c_unix_ts_2015 =      1420070400;

var
  DMMain: TDMMain;
  g_hAppMutex: THandle;

  procedure ShowInfo(const Msg:string);
  procedure ShowError(const Msg:string);
  function AskQuestion(const Msg:string):boolean;
  function OneInstance():boolean;

implementation

{%CLASSGROUP 'Vcl.Controls.TControl'}

{$R *.dfm}

uses UDlgMsg, UDMEnc, Winapi.Windows;

function AskQuestion(const Msg:string):boolean;
begin
  FormMsg.dxLayoutControlItemOk.Visible:=false;
  FormMsg.dxLayoutControlGroupYesNo.Visible:=true;
  FormMsg.cxMemo.Lines.Clear;
  FormMsg.cxMemo.Lines.Text:=Msg;
  FormMsg.dxLayoutControlGroupTab.ItemIndex:=2;
  FormMsg.ShowModal;
  Result:= FormMsg.Yes;
end;

procedure ShowInfo(const Msg:string);
begin
  FormMsg.dxLayoutControlGroupYesNo.Visible:=false;
  FormMsg.dxLayoutControlItemOk.Visible:=true;
  FormMsg.cxMemo.Lines.Clear;
  FormMsg.cxMemo.Lines.Text:=Msg;
  FormMsg.dxLayoutControlGroupTab.ItemIndex:=0;
  FormMsg.ShowModal;
end;

procedure ShowError(const Msg:string);
begin
  FormMsg.dxLayoutControlGroupYesNo.Visible:=false;
  FormMsg.dxLayoutControlItemOk.Visible:=true;
  FormMsg.cxMemo.Lines.Clear;
  FormMsg.cxMemo.Lines.Text:=Msg;
  FormMsg.dxLayoutControlGroupTab.ItemIndex:=1;
  FormMsg.ShowModal;
end;

function OneInstance():boolean;
var
  g_hAppCritSecMutex: THandle;
  dw: Longint;
begin
  g_hAppCritSecMutex:= CreateMutex(nil,true,PChar(Application.Title + '.OneInstance32.CriticalSection'));
  g_hAppMutex:= CreateMutex(nil,false,PChar(Application.Title + 'OneInstance32.Default'));
  dw:= WaitForSingleObject(g_hAppMutex,0);
  Result:= (dw <> WAIT_TIMEOUT);
  ReleaseMutex(g_hAppCritSecMutex);
  CloseHandle(g_hAppCritSecMutex);
end;

function TDMMain.CreateNotesTable():boolean;
begin
  if not UCMain.Connected then Result:=false
  else
  begin
    UCMain.AutoCommit:=false;
    UCMain.StartTransaction;
    try
      UniSQLNotes.Execute;
    except
      on E:Exception do
      begin
        UCMain.Rollback; UCMain.AutoCommit:=true;
        ShowError(E.Message);
        Result:=false; Exit;
      end;
    end;
    UCMain.Commit;
    UCMain.AutoCommit:=true;
    Result:=true;
  end;
end;

function TDMMain.NeedToSync:boolean;
begin
  if LQNotes.Active AND UQNotes.Active then
  begin
    LQNotes.First;
    while not LQNotes.Eof do
    begin
      if UQNotes.Locate('note_id',LQNotes.FieldByName('note_id').AsInteger,[]) then
      begin
        if ((UQNotes.FieldByName('info_hash').AsString<>LQNotes.FieldByName('info_hash').AsString)
        OR (UQNotes.FieldByName('name').AsString<>LQNotes.FieldByName('name').AsString)) then
        begin
          LQNotes.First; Result:=true; Exit;
        end;
      end
      else
      begin
        LQNotes.First; Result:=true; Exit;
      end;
      LQNotes.Next;
    end;
    UQNotes.First;
    while not UQNotes.Eof do
    begin
      if not LQNotes.Locate('note_id',UQNotes.FieldByName('note_id').AsInteger,[]) then
      begin
        LQNotes.First; Result:=true; Exit;
      end;
      UQNotes.Next;
    end;
    LQNotes.First;
    Result:=false;
  end
  else Result:=false;
end;

function TDMMain.SyncWithRemote:boolean;
var
  iNoteHstId, iNoteId:integer;
begin
  if LQNotes.Active AND UQNotes.Active then
  begin
    UCMain.AutoCommit:=false;
    UCMain.StartTransaction;
    LCLocal.StartTransaction;
    try
        LQNotes.First;
        while not LQNotes.Eof do
        begin
          if UQNotes.Locate('note_id',LQNotes.FieldByName('note_id').AsInteger,[]) then
          begin
            if UQNotes.FieldByName('info_hash').AsString<>LQNotes.FieldByName('info_hash').AsString then
            begin
              DMMain.LQNoteHstId.Close; DMMain.LQNoteHstId.Open;
              if DMMain.LQNoteHstId.IsEmpty
              then iNoteHstId:= 1
              else iNoteHstId:= DMMain.LQNoteHstId.FieldByName('note_hst_id').AsInteger + 1;
              LQNoteHstInsert.Prepare;
              LQNoteHstInsert.ParamByName('note_hst_id').AsInteger:= iNoteHstId;
              LQNoteHstInsert.ParamByName('note_id').AsInteger:= LQNotes.FieldByName('note_id').AsInteger;
              LQNoteHstInsert.ParamByName('created').AsDateTime:= LQNotes.FieldByName('updated').AsDateTime;
              LQNoteHstInsert.ParamByName('info').AsString:= LQNotes.FieldByName('info').AsString;
              LQNoteHstInsert.ParamByName('info_hash').AsString:= LQNotes.FieldByName('info_hash').AsString;
              LQNoteHstInsert.Execute;
              LQNoteHstInsert.UnPrepare;
              LQNoteUpdate.Prepare;
              LQNoteUpdate.ParamByName('note_id').AsInteger:= LQNotes.FieldByName('note_id').AsInteger;
              LQNoteUpdate.ParamByName('name').AsString:= UQNotes.FieldByName('name').AsString;
              LQNoteUpdate.ParamByName('info').AsString:= GetDec(UQNotes.FieldByName('info').AsString,'info','notes');
              LQNoteUpdate.ParamByName('info_hash').AsString:= UQNotes.FieldByName('info_hash').AsString;
              LQNoteUpdate.Execute;
              LQNoteUpdate.UnPrepare;
            end
            else
            if UQNotes.FieldByName('name').AsString<>LQNotes.FieldByName('name').AsString then
            begin
              LQNotes.Edit;
              LQNotes.FieldByName('name').AsString:= UQNotes.FieldByName('name').AsString;
              LQNotes.Post;
            end;
          end
          else
          begin
            LQNoteDelete.ParamByName('note_id').AsInteger:= LQNotes.FieldByName('note_id').AsInteger;
            LQNoteDelete.Execute;
          end;
          LQNotes.Next;
        end;
        UQNotes.First;
        while not UQNotes.Eof do
        begin
          if not LQNotes.Locate('note_id',UQNotes.FieldByName('note_id').AsInteger,[]) then
          begin
            iNoteId := UQNotes.FieldByName('note_id').AsInteger;
            (*
            DMMain.LQNoteId.Close; DMMain.LQNoteId.Open;
            if DMMain.LQNoteId.IsEmpty
            then iNoteId:= 1
            else iNoteId:= DMMain.LQNoteId.FieldByName('note_id').AsInteger + 1;
            *)
            DMMain.LQNoteInsert.Prepare;
            DMMain.LQNoteInsert.ParamByName('note_id').AsInteger:= iNoteId;
            DMMain.LQNoteInsert.ParamByName('name').AsString:= UQNotes.FieldByName('name').AsString;
            DMMain.LQNoteInsert.ParamByName('info').AsString:= GetDec(UQNotes.FieldByName('info').AsString,'info','notes');
            DMMain.LQNoteInsert.ParamByName('info_hash').AsString:= UQNotes.FieldByName('info_hash').AsString;
            DMMain.LQNoteInsert.Execute;
            DMMain.LQNoteInsert.UnPrepare;
          end;
          UQNotes.Next;
        end;
    except
      on E:Exception do
      begin
        LCLocal.Rollback; UCMain.Rollback; UCMain.AutoCommit:=true;
        Result:=false; Exit;
      end;
    end;
    LCLocal.Commit; UCMain.Commit; UCMain.AutoCommit:=true; Result:=true;
  end
  else Result:=false;
end;


function TDMMain.SyncWithLocal:boolean;
var
  iTempNoteId, iTs: integer;
begin
  if LQNotes.Active AND UQNotes.Active then
  begin
    iTempNoteId:= LQNotes.FieldByName('note_id').AsInteger;
    UQCurTs.Close;
    UQCurTs.Open;
    iTs:= (UQCurTs.FieldByName('ts').AsLargeInt - c_unix_ts_2015) mod c_int32_max_val;
    UCMain.AutoCommit:=false;
    UCMain.StartTransaction;
    LCLocal.StartTransaction;
    try
        LQNotes.First;
        while not LQNotes.Eof do
        begin
          if UQNotes.Locate('note_id',LQNotes.FieldByName('note_id').AsInteger,[]) then
          begin
            if UQNotes.FieldByName('info_hash').AsString<>LQNotes.FieldByName('info_hash').AsString then
            begin
              UQNoteUpdate.Prepare;
              UQNoteUpdate.ParamByName('note_id').AsInteger:= LQNotes.FieldByName('note_id').AsInteger;
              UQNoteUpdate.ParamByName('name').AsString:= LQNotes.FieldByName('name').AsString;
              UQNoteUpdate.ParamByName('info').AsString:= GetEnc(LQNotes.FieldByName('info').AsString,'info','notes');
              UQNoteUpdate.ParamByName('info_hash').AsString:= LQNotes.FieldByName('info_hash').AsString;
              UQNoteUpdate.ParamByName('updated').AsDateTime:= LQNotes.FieldByName('updated').AsDateTime;
              UQNoteUpdate.Execute;
              UQNoteUpdate.UnPrepare;
            end
            else
            if UQNotes.FieldByName('name').AsString<>LQNotes.FieldByName('name').AsString then
            begin
              UQNotes.Edit;
              UQNotes.FieldByName('name').AsString:= LQNotes.FieldByName('name').AsString;
              UQNotes.Post;
            end;
          end
          else
          begin
            UQNoteInsert.Prepare;
            UQNoteInsert.ParamByName('note_id').AsInteger:= LQNotes.FieldByName('note_id').AsInteger;
            UQNoteInsert.ParamByName('name').AsString:= LQNotes.FieldByName('name').AsString;
            UQNoteInsert.ParamByName('info').AsString:= GetEnc(LQNotes.FieldByName('info').AsString,'info','notes');
            UQNoteInsert.ParamByName('info_hash').AsString:= LQNotes.FieldByName('info_hash').AsString;
            UQNoteInsert.ParamByName('updated').AsDateTime:= LQNotes.FieldByName('updated').AsDateTime;
            UQNoteInsert.Execute;
            UQNoteInsert.UnPrepare;
          end;
          LQNotes.Next;
        end;
        UQNotes.First;
        while not UQNotes.Eof do
        begin
          if not LQNotes.Locate('note_id',UQNotes.FieldByName('note_id').AsInteger,[]) then
          begin
            UQNoteDelete.ParamByName('note_id').AsInteger:= UQNotes.FieldByName('note_id').AsInteger;
            UQNoteDelete.Execute;
          end;
          UQNotes.Next;
        end;
        UQLastSyncUpdate.Prepare;
        UQLastSyncUpdate.ParamByName('ts').AsInteger:= iTs;
        UQLastSyncUpdate.Execute;
        UQLastSyncUpdate.UnPrepare;
        LQNetwork.Edit;
        LQNetwork.FieldByName('ts').AsInteger:= iTs;
        LQNetwork.Post;
    except
      on E:Exception do
      begin
        LCLocal.Rollback; UCMain.Rollback; UCMain.AutoCommit:=true;
        Result:=false; Exit;
      end;
    end;
    LCLocal.Commit; UCMain.Commit; UCMain.AutoCommit:=true;
    LQNotes.Locate('note_id',iTempNoteId,[]);
    Result:=true;
  end
  else Result:=false;
end;


function TDMMain.ConnectLocal(Pwd:string):boolean;
var
  bFileExist: boolean;
begin
  bFileExist := FileExists(ExtractFilePath(Application.ExeName)+c_local_storage);
  try
    LCLocal.Database:= ExtractFilePath(Application.ExeName)+c_local_storage;
    LCLocal.EncryptionKey:= GetHash256(Pwd);
    LCLocal.Connect;
  except
    on E: Exception do
    begin
      if Pwd = '' then
      begin
        try
          LCLocal.EncryptionKey:= '';
          LCLocal.Connect;
          LCLocal.EncryptDatabase(GetHash256(''));
        except
          on E: Exception do
          begin
            ShowError('Access to local storage denied.');
            Result:=false; Exit;
          end;
        end;
      end
      else
      begin
        ShowError('Access to local storage denied.');
        Result:=false; Exit;
      end;
    end;
  end;
  if NOT bFileExist AND LCLocal.Connected then LDLocal.Restore;
  Result:=true;
end;


procedure TDMMain.ScSSHClientServerKeyValidate(Sender: TObject;
  NewServerKey: TScKey; var Accept: Boolean);
begin
  Accept:=true;
end;

function TDMMain.ConnectRemote():boolean;
begin
  if (not LQNetwork.Active) OR LQNetwork.IsEmpty then
  begin
    Result:=false; Exit;
  end;
  DMMain.UCMain.Disconnect;
  DMMain.ScSSHClient.Disconnect;
  if (Length(LQNetwork.FieldByName('ssh_host').AsString)>0) AND
  (Length(LQNetwork.FieldByName('ssh_user').AsString)>0) AND
  (DMMain.LQNetwork.FieldByName('ssh_port').AsInteger>0) then
  begin
    DMMain.ScSSHClient.HostName:= DMMain.LQNetwork.FieldByName('ssh_host').AsString;
    DMMain.ScSSHClient.User:= DMMain.LQNetwork.FieldByName('ssh_user').AsString;
    DMMain.ScSSHClient.Password:= DMMain.LQNetwork.FieldByName('ssh_pwd').AsString;
    DMMain.ScSSHClient.Port:=  DMMain.LQNetwork.FieldByName('ssh_port').AsInteger;
    if DMMain.ScRegStorage.Keys.FindKey(c_priv_key_name) <> nil then
    begin
      DMMain.ScSSHClient.Authentication := ScSSHUtil.atPublicKey;
      DMMain.ScSSHClient.PrivateKeyName := c_priv_key_name;
      try
        DMMain.ScSSHClient.Connect;
      except
        on E: Exception do
        begin
            DMMain.ScSSHClient.Authentication := ScSSHUtil.atPassword;
            try
              DMMain.ScSSHClient.Connect;
            except
              on E: Exception do
              begin
                ShowError('SSH connection error.'+#13+E.Message);
                Result:=false; Exit;
              end;
            end;
        end;
      end;
    end
    else
    begin
      DMMain.ScSSHClient.Authentication := ScSSHUtil.atPassword;
      try
        DMMain.ScSSHClient.Connect;
      except
        on E: Exception do
        begin
          ShowError('SSH connection error.'+#13+E.Message);
          Result:=false; Exit;
        end;
      end;
    end;
  end;
  if (Length(LQNetwork.FieldByName('db_fw_host').AsString)>0) AND
  (Length(LQNetwork.FieldByName('db_fw_port').AsString)>0) then
  begin
    if ScSSHClient.Connected then
    begin
      DMMain.UCMain.Server:= '127.0.0.1';
      DMMain.UCMain.Port:= 3308;
      DMMain.ScSSHChannel.SourcePort:= 3308;
      DMMain.ScSSHChannel.DestPort:= DMMain.LQNetwork.FieldByName('db_fw_port').AsInteger;
      DMMain.ScSSHChannel.DestHost:= DMMain.LQNetwork.FieldByName('db_fw_host').AsString;
      try
        DMMain.ScSSHChannel.Connect;
      except
        on E: Exception do
        begin
          ShowError('SSH port forwarding error.'+#13+'(local port 3308)'
          +#13+E.Message);
          Result:=false; Exit;
        end;
      end;
    end
    else
    begin
      DMMain.UCMain.Server:= DMMain.LQNetwork.FieldByName('db_fw_host').AsString;
      DMMain.UCMain.Port:= DMMain.LQNetwork.FieldByName('db_fw_port').AsInteger;
    end;
  end
  else
  begin
    Result:= false;
    Exit;
  end;
  if (Length(LQNetwork.FieldByName('db_name').AsString)>0) AND
  (Length(LQNetwork.FieldByName('db_user').AsString)>0) then
  begin
    DMMain.UCMain.Database:= DMMain.LQNetwork.FieldByName('db_name').AsString;
    DMMain.UCMain.Username:= DMMain.LQNetwork.FieldByName('db_user').AsString;
    DMMain.UCMain.Password:= DMMain.LQNetwork.FieldByName('db_pwd').AsString;
    try
      DMMain.UCMain.Connect;
      Result:= true;
    except
      on E: Exception do
      begin
        ShowError('MySQL connection error.'+#13+E.Message);
        Result:=false; Exit;
      end;
    end;
  end
  else Result:= false;
end;

end.
