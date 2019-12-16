unit UMain;

interface

uses
  Winapi.Windows, Winapi.Messages, System.SysUtils, System.Variants, System.Classes, Vcl.Graphics,
  Vcl.Controls, Vcl.Forms, Vcl.Dialogs, cxGraphics, cxControls, cxLookAndFeels,
  cxLookAndFeelPainters, dxSkinsCore, dxSkinDevExpressDarkStyle,
  dxSkinDevExpressStyle, dxSkinFoggy, dxSkinLondonLiquidSky, dxSkinMcSkin,
  dxSkinOffice2010Blue, dxSkinOffice2010Silver, dxSkinOffice2013DarkGray,
  dxSkinOffice2013White, dxSkinSevenClassic, dxSkinSharp, dxSkinSharpPlus,
  dxSkinsDefaultPainters, dxSkinVS2010, dxSkinWhiteprint, dxSkinscxPCPainter,
  dxLayoutContainer, dxLayoutControl, dxLayoutcxEditAdapters,
  dxLayoutControlAdapters, cxContainer, cxEdit, Vcl.Menus, Vcl.StdCtrls,
  cxButtons, cxTextEdit, dxSkinsdxBarPainter, dxBar, cxClasses,
  dxSkinsdxStatusBarPainter, dxStatusBar, cxMaskEdit, cxDropDownEdit,
  cxLookupEdit, cxDBLookupEdit, cxDBLookupComboBox, cxStyles, cxCustomData,
  cxFilter, cxData, cxDataStorage, cxNavigator, Data.DB, cxDBData,
  cxGridCustomTableView, cxGridTableView, cxGridDBTableView, cxGridLevel,
  cxGridCustomView, cxGrid, cxCalendar, cxCheckBox, DBAccess, Uni, MemDS,
  cxCurrencyEdit, cxButtonEdit, Vcl.ExtCtrls, cxTimeEdit, cxMemo, Vcl.ComCtrls,
  dxCore, cxDateUtils, dxSkinBlack;

type
  TFormMain = class(TForm)
    dxLayoutControlGroup_Root: TdxLayoutGroup;
    dxLayoutControl: TdxLayoutControl;
    dxLayoutControlGroup3: TdxLayoutGroup;
    dxLayoutControlItem1: TdxLayoutItem;
    dxLayoutControlGroupDefault: TdxLayoutGroup;
    dxLayoutControlItem2: TdxLayoutItem;
    cxTextEditPwd: TcxTextEdit;
    cxButtonPwdSubmit: TcxButton;
    dxLayoutControlGroupHotMenu: TdxLayoutGroup;
    dxStatusBar: TdxStatusBar;
    dxStatusBarContainer3: TdxStatusBarContainerControl;
    cxButtonChangePwd: TcxButton;
    dxStatusBarContainer4: TdxStatusBarContainerControl;
    cxButtonConnect: TcxButton;
    cxGridNotesDBTableView1: TcxGridDBTableView;
    cxGridNotesLevel1: TcxGridLevel;
    cxGridNotes: TcxGrid;
    dxLayoutControlItemNotes: TdxLayoutItem;
    cxGridNotesDBTableView1ColumnNoteId: TcxGridDBColumn;
    cxGridNotesDBTableView1Column2: TcxGridDBColumn;
    cxGridNotesDBTableView1Column3: TcxGridDBColumn;
    cxMemoNoteInfo: TcxMemo;
    cxTextEditNoteName: TcxTextEdit;
    dxLayoutControlItemName: TdxLayoutItem;
    dxLayoutControlItemMemo: TdxLayoutItem;
    cxButtonApply: TcxButton;
    cxButtonCancel: TcxButton;
    cxButtonNew: TcxButton;
    cxButtonDelete: TcxButton;
    dxLayoutControlGroup1: TdxLayoutGroup;
    dxLayoutControlItemCancel: TdxLayoutItem;
    dxLayoutControlItemApply: TdxLayoutItem;
    dxLayoutControlGroup2: TdxLayoutGroup;
    dxLayoutControlItemNew: TdxLayoutItem;
    dxLayoutControlItemRemove: TdxLayoutItem;
    cxTextEditLen: TcxTextEdit;
    dxLayoutControlItemLen: TdxLayoutItem;
    dxLayoutControlGroup4: TdxLayoutGroup;
    dxLayoutControlItemUpdated: TdxLayoutItem;
    cxDateEditUpdated: TcxTextEdit;
    dxLayoutControlItemHistory: TdxLayoutItem;
    cxButtonHistory: TcxButton;
    dxStatusBarContainer5: TdxStatusBarContainerControl;
    cxButtonBackup: TcxButton;
    TimerConnect: TTimer;
    dxLayoutControlGroupConnect: TdxLayoutGroup;
    dxLayoutControlLabeledItemInfo: TdxLayoutLabeledItem;
    dxLayoutControlGroupStorage: TdxLayoutGroup;
    dxLayoutControlLabeledItem1: TdxLayoutLabeledItem;
    dxLayoutControlItem3: TdxLayoutItem;
    dxLayoutControlGroup7: TdxLayoutGroup;
    cxButtonNewStorage: TcxButton;
    dxLayoutControlLabeledItem2: TdxLayoutLabeledItem;
    dxLayoutControlGroupSync: TdxLayoutGroup;
    dxLayoutControlGroup6: TdxLayoutGroup;
    dxLayoutControlGroup8: TdxLayoutGroup;
    cxButtonRestore: TcxButton;
    dxStatusBarContainer9: TdxStatusBarContainerControl;
    procedure cxButtonPwdSubmitClick(Sender: TObject);
    procedure cxTextEditPwdKeyDown(Sender: TObject; var Key: Word;
      Shift: TShiftState);
    procedure FormCreate(Sender: TObject);
    procedure FormShow(Sender: TObject);
    procedure FormClose(Sender: TObject; var Action: TCloseAction);
    procedure cxButtonChangePwdClick(Sender: TObject);
    procedure cxMemoNoteInfoPropertiesChange(Sender: TObject);
    procedure cxButtonNewClick(Sender: TObject);
    procedure cxTextEditNoteNamePropertiesChange(Sender: TObject);
    procedure cxButtonCancelClick(Sender: TObject);
    procedure cxButtonApplyClick(Sender: TObject);
    procedure cxButtonDeleteClick(Sender: TObject);
    procedure cxGridNotesDBTableView1FocusedRecordChanged(
      Sender: TcxCustomGridTableView; APrevFocusedRecord,
      AFocusedRecord: TcxCustomGridRecord;
      ANewItemRecordFocusingChanged: Boolean);
    procedure cxButtonHistoryClick(Sender: TObject);
    procedure cxButtonConnectClick(Sender: TObject);
    procedure cxButtonBackupClick(Sender: TObject);
    procedure TimerConnectTimer(Sender: TObject);
    procedure cxButtonNewStorageClick(Sender: TObject);
    procedure cxButtonRestoreClick(Sender: TObject);
  private
    _note_id: integer;
    _info_changed: boolean;
    { Private declarations }
    procedure RefreshNote(fUpdateData: boolean = true);
    procedure CheckFields;
    procedure ShowNotes;
  public
    { Public declarations }
  end;

var
  FormMain: TFormMain;

implementation

{$R *.dfm}

uses UDMMain, UDMEnc, UDlgPwd, UDlgHistory, UDlgConnection;

procedure TFormMain.CheckFields;
begin
  if DMMain.LQNotes.Active AND (NOT DMMain.LQNotes.IsEmpty) then
  begin
    dxLayoutControlItemApply.Enabled:= (Length(cxTextEditNoteName.Text)>0) AND
    (Length(cxMemoNoteInfo.Lines.Text)>0) AND
    ((DMMain.LQNotes.FieldByName('name').AsString<>cxTextEditNoteName.Text)
    OR (DMMain.LQNotes.FieldByName('info').AsString<>cxMemoNoteInfo.Lines.Text)
    OR (_note_id=-1));
    dxLayoutControlItemCancel.Enabled:= dxLayoutControlItemApply.Enabled;
  end
  else
  begin
    dxLayoutControlItemApply.Enabled:= (Length(cxTextEditNoteName.Text)>0) AND
    (Length(cxMemoNoteInfo.Lines.Text)>0);
    dxLayoutControlItemCancel.Enabled:= dxLayoutControlItemApply.Enabled;
  end;
end;

procedure TFormMain.RefreshNote(fUpdateData: boolean = true);
begin
  if fUpdateData then
  begin
    cxTextEditNoteName.Text:='';
    cxMemoNoteInfo.Lines.Text:='';
    cxTextEditLen.Text:='';
    cxDateEditUpdated.Text:='';
  end;
  dxLayoutControlItemUpdated.Enabled:=false; dxLayoutControlItemLen.Enabled:= false;
  dxLayoutControlItemName.Enabled:= DMMain.LQNotes.Active AND (NOT DMMain.LQNotes.IsEmpty);
  dxLayoutControlItemMemo.Enabled:= DMMain.LQNotes.Active AND (NOT DMMain.LQNotes.IsEmpty);
  dxLayoutControlItemApply.Enabled:= false;
  dxLayoutControlItemCancel.Enabled:= false;
  dxLayoutControlItemNew.Enabled:= DMMain.LQNotes.Active;
  dxLayoutControlItemRemove.Enabled:= DMMain.LQNotes.Active AND (NOT DMMain.LQNotes.IsEmpty);
  dxLayoutControlItemNotes.Enabled:= DMMain.LQNotes.Active AND (NOT DMMain.LQNotes.IsEmpty);
  dxLayoutControlItemHistory.Enabled:= DMMain.LQNotes.Active AND (NOT DMMain.LQNotes.IsEmpty)
  AND (DMMain.LQNotes.FieldByName('sq_note_id').AsInteger>0);
  if DMMain.LQNotes.Active AND (NOT DMMain.LQNotes.IsEmpty) AND fUpdateData then
  begin
    cxTextEditNoteName.Text:= DMMain.LQNotes.FieldByName('name').AsString;
    cxMemoNoteInfo.Lines.Text:= DMMain.LQNotes.FieldByName('info').AsString;
    cxTextEditLen.Text:= IntToStr(Length(DMMain.LQNotes.FieldByName('info').AsString))+'/8192';
    cxDateEditUpdated.Text:= formatdatetime('c',DMMain.LQNotes.FieldByName('updated').AsDateTime);
    cxTextEditNoteName.Update;
    cxMemoNoteInfo.Update;
    cxTextEditLen.Update;
    cxDateEditUpdated.Update;
  end;
end;

procedure TFormMain.TimerConnectTimer(Sender: TObject);
begin
  TimerConnect.Enabled:=false;
  dxLayoutControlGroupHotMenu.Visible:=false;
  dxLayoutControlGroupConnect.Visible:=true;
  Update;
  if DMMain.ConnectRemote() then
  begin
    DMMain.UQNotes.Close;
    try
      DMMain.UQNotes.Open;
    except
      on E: Exception do
      begin
        if DMMain.CreateNotesTable() then
        begin
          DMMain.UQNotes.Close;
          try
            DMMain.UQNotes.Open;
            DMMain.SyncWithLocal;
            DMMain.UQNotes.Refresh;
          except
            on E: Exception do
            begin
              DMMain.UCMain.Disconnect;
              ShowError(E.Message);
              dxLayoutControlGroupHotMenu.Visible:=true;
              dxLayoutControlGroupConnect.Visible:=false;
              Exit;
            end;
          end;
        end
        else
        begin
          DMMain.UCMain.Disconnect;
          ShowError(E.Message);
          dxLayoutControlGroupHotMenu.Visible:=true;
          dxLayoutControlGroupConnect.Visible:=false;
          Exit;
        end;
      end;
    end;
    Update;
    if DMMain.NeedToSync() then
    begin
      _info_changed:= true;
      DMMain.UQLastSync.Close;
      DMMain.UQLastSync.Open;
      if DMMain.UQLastSync.FieldByName('ts').AsInteger >
      DMMain.LQNetwork.FieldByName('ts').AsInteger then
      begin
        if AskQuestion('It seems like your remote storage has fresher version of data.'+
        #13+'Would you like to restore data from remote storage?') then
        begin
          if DMMain.SyncWithRemote then
          begin
            DMMain.LQNotes.Refresh;
            _info_changed:=false;
            DMMain.LQNetwork.Edit;
            DMMain.LQNetwork.FieldByName('ts').AsInteger:= DMMain.UQLastSync.FieldByName('ts').AsInteger;
            DMMain.LQNetwork.Post;
            ShowInfo('Data successfully restored.');
          end;
        end;
      end;
      cxButtonRestore.Enabled:= _info_changed AND DMMain.UCMain.Connected;
      cxButtonBackup.Enabled:= _info_changed AND DMMain.UCMain.Connected;
    end;
  end;
  dxStatusBar.Visible:=true;
  dxLayoutControlGroupHotMenu.Visible:=true;
  dxLayoutControlGroupConnect.Visible:=false;
  Update;
end;

procedure TFormMain.cxButtonApplyClick(Sender: TObject);
var
  iNoteId, iNoteHstId:integer;
begin
  if _note_id = -1 then
  begin
    DMMain.LQNoteId.Close; DMMain.LQNoteId.Open;
    if DMMain.LQNoteId.IsEmpty
    then _note_id:= 1
    else _note_id:= DMMain.LQNoteId.FieldByName('note_id').AsInteger + 1;
    try
      DMMain.LQNoteInsert.Prepare;
      DMMain.LQNoteInsert.ParamByName('note_id').AsInteger:= _note_id;
      DMMain.LQNoteInsert.ParamByName('name').AsString:= cxTextEditNoteName.Text;
      DMMain.LQNoteInsert.ParamByName('info').AsString:= cxMemoNoteInfo.Lines.Text;
      DMMain.LQNoteInsert.ParamByName('info_hash').AsString:= GetDirectHash256(cxMemoNoteInfo.Lines.Text);
      DMMain.LQNoteInsert.Execute;
      _info_changed:=true; cxButtonBackup.Enabled:= _info_changed AND DMMain.UCMain.Connected;
      ShowInfo('Note has been successfully added.');
    except
      on E:Exception do
      begin
        ShowError(E.Message);
        DMMain.LQNoteInsert.UnPrepare;
        Exit;
      end;
    end;
    DMMain.LQNoteInsert.UnPrepare;
  end
  else
  if _note_id > 0 then
  begin
    try
      if DMMain.LQNotes.FieldByName('info_hash').AsString<>GetDirectHash256(cxMemoNoteInfo.Lines.Text) then
      begin
        DMMain.LQNoteHstId.Close; DMMain.LQNoteHstId.Open;
        if DMMain.LQNoteHstId.IsEmpty
        then iNoteHstId:= 1
        else iNoteHstId:= DMMain.LQNoteHstId.FieldByName('note_hst_id').AsInteger + 1;
        DMMain.LQNoteHstInsert.Prepare;
        DMMain.LQNoteHstInsert.ParamByName('note_hst_id').AsInteger:= iNoteHstId;
        DMMain.LQNoteHstInsert.ParamByName('note_id').AsInteger:= _note_id;
        DMMain.LQNoteHstInsert.ParamByName('created').AsDateTime:= DMMain.LQNotes.FieldByName('updated').AsDateTime;
        DMMain.LQNoteHstInsert.ParamByName('info').AsString:= DMMain.LQNotes.FieldByName('info').AsString;
        DMMain.LQNoteHstInsert.ParamByName('info_hash').AsString:= DMMain.LQNotes.FieldByName('info_hash').AsString;
        DMMain.LQNoteHstInsert.Execute;
      end;
      DMMain.LQNoteUpdate.Prepare;
      DMMain.LQNoteUpdate.ParamByName('note_id').AsInteger:= _note_id;
      DMMain.LQNoteUpdate.ParamByName('name').AsString:= cxTextEditNoteName.Text;
      DMMain.LQNoteUpdate.ParamByName('info').AsString:= cxMemoNoteInfo.Lines.Text;
      DMMain.LQNoteUpdate.ParamByName('info_hash').AsString:= GetDirectHash256(cxMemoNoteInfo.Lines.Text);
      DMMain.LQNoteUpdate.Execute;
      _info_changed:=true; cxButtonBackup.Enabled:= _info_changed AND DMMain.UCMain.Connected;
      ShowInfo('Note has been successfully updated.');
    except
      on E:Exception do
      begin
        ShowError(E.Message);
        DMMain.LQNoteHstInsert.UnPrepare;
        DMMain.LQNoteUpdate.UnPrepare;
        Exit;
      end;
    end;
    DMMain.LQNoteHstInsert.UnPrepare;
    DMMain.LQNoteUpdate.UnPrepare;
  end;
  iNoteId:= _note_id;
  DMMain.LQNotes.Refresh;
  DMMain.LQNotes.Locate('note_id',iNoteId,[]);
  dxLayoutControlItemNotes.Enabled:= DMMain.LQNotes.Active AND (NOT DMMain.LQNotes.IsEmpty);
  dxLayoutControlItemHistory.Enabled:= DMMain.LQNotes.Active AND (NOT DMMain.LQNotes.IsEmpty)
  AND (DMMain.LQNotes.FieldByName('sq_note_id').AsInteger>0);
  dxLayoutControlItemRemove.Enabled:= DMMain.LQNotes.Active AND (NOT DMMain.LQNotes.IsEmpty);
  dxLayoutControlItemNew.Enabled:= true;
  RefreshNote(false);
end;

procedure TFormMain.cxButtonCancelClick(Sender: TObject);
begin
  dxLayoutControlItemNotes.Enabled:= DMMain.LQNotes.Active AND (NOT DMMain.LQNotes.IsEmpty);
  dxLayoutControlItemHistory.Enabled:= DMMain.LQNotes.Active AND (NOT DMMain.LQNotes.IsEmpty)
  AND (DMMain.LQNotes.FieldByName('sq_note_id').AsInteger>0);
  dxLayoutControlItemRemove.Enabled:= DMMain.LQNotes.Active AND (NOT DMMain.LQNotes.IsEmpty);
  dxLayoutControlItemNew.Enabled:= true;
  RefreshNote;
end;

procedure TFormMain.cxButtonChangePwdClick(Sender: TObject);
begin
  FormPwd.Pwd:= '';
  if FormPwd.ShowModal = mrOk then
  begin
    try
      DMMain.LCLocal.EncryptDatabase(GetHash256(FormPwd.Pwd));
      ShowInfo('Password was successfully changed.');
    except
      on E: Exception do
      begin
        ShowError(E.Message);
      end;
    end;
  end;
end;

procedure TFormMain.cxButtonConnectClick(Sender: TObject);
begin
  if FormConnection.ShowModal=mrOk then
  begin
    DMEnc.Salt:= DMMain.LQNetwork.FieldByName('salt').AsString;
    TimerConnect.Enabled:= true;
  end;
end;

procedure TFormMain.cxButtonDeleteClick(Sender: TObject);
begin
  if AskQuestion('Are you sure you want to remove this note?') then
  begin
    if _note_id > 0 then
    begin
      try
        DMMain.LQNoteDelete.Prepare;
        DMMain.LQNoteDelete.ParamByName('note_id').AsInteger:= DMMain.LQNotes.FieldByName('note_id').AsInteger;
        DMMain.LQNoteDelete.Execute;
        DMMain.LQNoteDelete.UnPrepare;
        DMMain.LQNotes.Refresh;
        RefreshNote;
        dxLayoutControlItemNotes.Enabled:= DMMain.LQNotes.Active AND (NOT DMMain.LQNotes.IsEmpty);
        dxLayoutControlItemHistory.Enabled:= DMMain.LQNotes.Active AND (NOT DMMain.LQNotes.IsEmpty)
        AND (DMMain.LQNotes.FieldByName('sq_note_id').AsInteger>0);
        dxLayoutControlItemRemove.Enabled:= DMMain.LQNotes.Active AND (NOT DMMain.LQNotes.IsEmpty);
        cxButtonBackup.Enabled:= DMMain.UCMain.Connected;
        ShowInfo('Note has been successfully removed.');
        _info_changed:=true;
      except
        on E:Exception do
        begin
          ShowError(E.Message);
          DMMain.LQNoteDelete.UnPrepare;
          Exit;
        end;
      end;
    end;
  end;
end;

procedure TFormMain.cxButtonHistoryClick(Sender: TObject);
begin
  FormHistory.NoteId:= DMMain.LQNotes.FieldByName('note_id').AsInteger;
  if FormHistory.ShowModal = mrOk then cxMemoNoteInfo.Lines.Text:= FormHistory.Info;
end;

procedure TFormMain.cxButtonNewClick(Sender: TObject);
begin
  cxTextEditNoteName.Text:=''; cxMemoNoteInfo.Lines.Text:='';
  cxTextEditLen.Text:='0/8192'; cxDateEditUpdated.Text:='';
  cxTextEditNoteName.Update; cxMemoNoteInfo.Update;
  cxTextEditLen.Update; cxDateEditUpdated.Update;
  dxLayoutControlItemUpdated.Enabled:=false;
  dxLayoutControlItemLen.Enabled:= false;
  dxLayoutControlItemNotes.Enabled:=false;
  dxLayoutControlItemHistory.Enabled:=false;
  dxLayoutControlItemName.Enabled:= true;
  dxLayoutControlItemMemo.Enabled:= true;
  dxLayoutControlItemApply.Enabled:= false;
  dxLayoutControlItemCancel.Enabled:= true;
  dxLayoutControlItemNotes.Enabled:=false;
  dxLayoutControlItemNew.Enabled:=false;
  dxLayoutControlItemRemove.Enabled:=false;
  _note_id:= -1;
end;

procedure TFormMain.cxButtonNewStorageClick(Sender: TObject);
begin
  FormPwd.Pwd:= '';
  if FormPwd.ShowModal = mrOk then
  begin
    try
      DMMain.LCLocal.Options.ForceCreateDatabase:= true;
      DMMain.ConnectLocal('');
      DMMain.LCLocal.Options.ForceCreateDatabase:= false;
      DMMain.LCLocal.EncryptDatabase(GetHash256(FormPwd.Pwd));
      ShowNotes;
    except
      on E: Exception do
      begin
        ShowError(E.Message);
      end;
    end;
  end;
end;

procedure TFormMain.ShowNotes;
begin
  try
    DMMain.LQNotes.Open;
    dxLayoutControlGroupDefault.Visible:=false;
    dxLayoutControlGroupHotMenu.Visible:=false;
    dxLayoutControlGroupStorage.Visible:=false;
    dxLayoutControlGroupConnect.Visible:=true;
    DMMain.LQNetwork.Open;
    DMEnc.Salt:= DMMain.LQNetwork.FieldByName('salt').AsString;
    TimerConnect.Enabled:=true;
    RefreshNote;

  except
    on E: Exception do
    begin
      ShowError(E.Message);
    end;
  end;
end;

procedure TFormMain.cxButtonPwdSubmitClick(Sender: TObject);
begin
  if DMMain.ConnectLocal(cxTextEditPwd.Text) then ShowNotes;
  cxTextEditPwd.Text:='';
end;

procedure TFormMain.cxButtonRestoreClick(Sender: TObject);
var
  bSync: boolean;
begin
  if AskQuestion('Would you like to restore data from remote storage?') then
  begin
    dxLayoutControlGroupHotMenu.Visible:=false;
    dxLayoutControlGroupSync.Visible:=true;
    Update;
    bSync := DMMain.SyncWithRemote;
    dxLayoutControlGroupSync.Visible:=false;
    dxLayoutControlGroupHotMenu.Visible:=true;
    Update;
    if bSync then
    begin
      DMMain.LQNotes.Refresh;
      _info_changed:=false;
      cxButtonBackup.Enabled:= _info_changed AND DMMain.UCMain.Connected;
      cxButtonRestore.Enabled:= _info_changed AND DMMain.UCMain.Connected;
      ShowInfo('Data successfully restored.');
    end;
  end;
end;

procedure TFormMain.cxButtonBackupClick(Sender: TObject);
var
  bSync, bConnect: boolean;
begin
  if NOT DMMain.UCMain.Connected then exit;
  if NOT (_info_changed AND
  AskQuestion('Would you like to save data on remote storage?'))
  then exit;
  dxLayoutControlGroupHotMenu.Visible:=false;
  dxLayoutControlGroupSync.Visible:=true;
  Update;
  bConnect := DMMain.ConnectRemote();
  if NOT bConnect then
  begin
    dxLayoutControlGroupSync.Visible:=false;
    dxLayoutControlGroupHotMenu.Visible:=true;
    Update;
    exit;
  end;
  cxGridNotesDBTableView1.BeginUpdate();
  DMMain.UQNotes.Close;
  try
    DMMain.UQNotes.Open;
  except
    on E: Exception do
    begin
      if DMMain.CreateNotesTable() then
      begin
        DMMain.UQNotes.Close;
        try
          DMMain.UQNotes.Open;
        except
          on E: Exception do
          begin
            //DMMain.UCMain.Disconnect;
            cxGridNotesDBTableView1.EndUpdate;
            ShowError(E.Message);
            exit;
          end;
        end;
      end;
    end;
  end;
  bSync := DMMain.SyncWithLocal;
  cxGridNotesDBTableView1.EndUpdate;
  dxLayoutControlGroupSync.Visible:=false;
  dxLayoutControlGroupHotMenu.Visible:=true;
  Update;
  if bSync then
  begin
    _info_changed:=false;
    cxButtonBackup.Enabled:= _info_changed AND DMMain.UCMain.Connected;
    cxButtonRestore.Enabled:= _info_changed AND DMMain.UCMain.Connected;
    ShowInfo('Data successfully saved.');
  end;
end;

procedure TFormMain.cxGridNotesDBTableView1FocusedRecordChanged(
  Sender: TcxCustomGridTableView; APrevFocusedRecord,
  AFocusedRecord: TcxCustomGridRecord; ANewItemRecordFocusingChanged: Boolean);
begin
  if DMMain.LQNotes.Active AND (NOT DMMain.LQNotes.IsEmpty) AND
  (_note_id <> AFocusedRecord.Values[cxGridNotesDBTableView1ColumnNoteId.Index]) then
  begin
    _note_id:= AFocusedRecord.Values[cxGridNotesDBTableView1ColumnNoteId.Index];
    RefreshNote;
  end;
end;

procedure TFormMain.cxMemoNoteInfoPropertiesChange(Sender: TObject);
begin
  cxTextEditLen.Text:= IntToStr(Length(cxMemoNoteInfo.Lines.Text))+'/8192';
  CheckFields;
end;

procedure TFormMain.cxTextEditNoteNamePropertiesChange(Sender: TObject);
begin
  CheckFields;
end;

procedure TFormMain.cxTextEditPwdKeyDown(Sender: TObject; var Key: Word;
  Shift: TShiftState);
begin
  if Ord(Key)=13 then  cxButtonPwdSubmitClick(Sender);
end;

procedure TFormMain.FormClose(Sender: TObject; var Action: TCloseAction);
var
  bSync, bConnect: boolean;
begin
  if DMMain.UCMain.Connected then
  begin
    if _info_changed AND
    AskQuestion('Would you like to save your data changes to remote database?') then
    begin
      dxLayoutControlGroupHotMenu.Visible:=false;
      dxLayoutControlGroupSync.Visible:=true;
      Update;
      bConnect := DMMain.ConnectRemote();
      if bConnect then
      begin
        cxGridNotesDBTableView1.BeginUpdate();
        Update;
        DMMain.UQNotes.Close;
        try
          DMMain.UQNotes.Open;
        except
          on E: Exception do
          begin
            if DMMain.CreateNotesTable() then
            begin
              DMMain.UQNotes.Close;
              DMMain.UQNotes.Open;
            end;
          end;
        end;
        DMMain.UQNotes.Refresh;
        bSync := DMMain.SyncWithLocal;
        cxGridNotesDBTableView1.EndUpdate;
        dxLayoutControlGroupSync.Visible:=false;
        dxLayoutControlGroupHotMenu.Visible:=true;
        Update;
        if bSync then
        begin
          _info_changed:=false;
          cxButtonBackup.Enabled:= _info_changed AND DMMain.UCMain.Connected;
          ShowInfo('Data successfully synchronized.');
        end;
      end
      else
      begin
        dxLayoutControlGroupSync.Visible:=false;
        dxLayoutControlGroupHotMenu.Visible:=true;
        Update;
      end;
    end;
  end;
  DMMain.LQNotes.Close; DMMain.LQNetwork.Close;
  DMMain.UQNotes.Close;
  DMMain.UCMain.Disconnect;
  DMMain.ScSSHChannel.Disconnect;
  DMMain.ScSSHClient.Disconnect;
end;

procedure TFormMain.FormCreate(Sender: TObject);
begin
  dxLayoutControlGroupDefault.AlignVert:= avCenter;
  _info_changed:=false;
  cxButtonBackup.Enabled:=false;
  cxButtonRestore.Enabled:=false;
end;

procedure TFormMain.FormShow(Sender: TObject);
begin
  if not OneInstance then
  begin
    ShowError('Application already opened.'+#13+'Execution of second instance restricted.');
    Application.Terminate; Exit;
  end;
  if not FileExists(ExtractFilePath(Application.ExeName)+c_local_storage) then
  begin
    dxLayoutControlGroupStorage.Visible:=true;
    dxLayoutControlGroupDefault.Visible:=false;
  end
  else
  begin
    dxLayoutControlGroupStorage.Visible:=false;
    dxLayoutControlGroupDefault.Visible:=true;
  end;
  dxLayoutControlGroupHotMenu.Visible:=false;
  dxLayoutControlGroupConnect.Visible:=false;
  dxLayoutControlGroupSync.Visible:=false;
  dxStatusBar.Visible:=false;
end;

end.
