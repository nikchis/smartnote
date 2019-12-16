unit UDMEnc;

interface

uses
  System.SysUtils, System.Classes, ipccore, ipcaes, Data.DB, MemDS,
  DBAccess, Uni, ipchash;

type
  TDMEnc = class(TDataModule)
    ipcHashSha3_224: TipcHash;
    ipcHashSha3_256: TipcHash;
    ipcAESext: TipcAES;
    procedure DataModuleCreate(Sender: TObject);
  private
    _salt: string;
    procedure SetSalt(const Value:string);
    { Private declarations }
  public
    property Salt: string read _salt write SetSalt;
    { Public declarations }
  end;

const
  prekey  = 'H9L1N8f3b9Z1x8cF';
  dec_err = '#ENCRYPTED';

var
  DMEnc: TDMEnc;
  function GetHash224(Msg:string):string;
  function GetHash256(Msg:string):string;
  function GetDirectHash256(const Text:string):string;
  function GetEnc(Query:TUniQuery;const Field:string;const Table:string):string;overload;
  function GetEnc(const Text:string;const Field:string;const Table:string):string;overload;
  function GetDec(Query:TUniQuery;const Field:string;const Table:string):string;overload;
  function GetDec(const Text:string;const Field:string;const Table:string):string;overload;

implementation

{%CLASSGROUP 'Vcl.Controls.TControl'}

{$R *.dfm}

uses UDMMain, System.StrUtils;

procedure TDMEnc.SetSalt(const Value:string);
begin
  _salt:= Value;
end;

function GetEnc(Query:TUniQuery;const Field:string;const Table:string):string;
begin
  if Length(Query.FieldByName(Field).AsString)=0 then Result:=''
  else
  begin
    try
      DMEnc.ipcAESext.KeyPassword:= GetHash224(Table+prekey+Field);
      DMEnc.ipcAESext.InputMessage:= Query.FieldByName(Field).AsString;
      DMEnc.ipcAESext.Encrypt;
      Result:= DMEnc.ipcAESext.OutputMessage;
    except
      on E:exception do
      begin
        ShowError(mdlg_err_enc_err);
        Result:='';
      end;
    end;
  end;
end;

function GetEnc(const Text:string;const Field:string;const Table:string):string;
begin
  if Length(Text)=0 then Result:=''
  else
  begin
    try
      DMEnc.ipcAESext.KeyPassword:= GetHash224(Table+prekey+Field);
      DMEnc.ipcAESext.InputMessage:= Text;
      DMEnc.ipcAESext.Encrypt;
      Result:= DMEnc.ipcAESext.OutputMessage;
    except
      on E:exception do
      begin
        ShowError(mdlg_err_enc_err);
        Result:='';
      end;
    end;
  end;
end;

function GetDec(Query:TUniQuery;const Field:string;const Table:string):string;
begin
  if Length(Query.FieldByName(Field).AsString)=0 then Result:=''
  else
  begin
    try
      DMEnc.ipcAESext.KeyPassword:= GetHash224(Table+prekey+Field);
      DMEnc.ipcAESext.InputMessage:= Query.FieldByName(Field).AsString;
      DMEnc.ipcAESext.Decrypt;
      Result:= DMEnc.ipcAESext.OutputMessage;
    except
      on E:exception do Result:= dec_err;
    end;
  end;
end;

function GetDec(const Text:string;const Field:string;const Table:string):string;
begin
  if Length(Text)=0 then Result:=''
  else
  begin
    try
      DMEnc.ipcAESext.KeyPassword:= GetHash224(Table+prekey+Field);
      DMEnc.ipcAESext.InputMessage:= Text;
      DMEnc.ipcAESext.Decrypt;
      Result:= DMEnc.ipcAESext.OutputMessage;
    except
      on E:exception do Result:= dec_err;
    end;
  end;
end;


function GetHash224(Msg:string):string;
begin
  DMEnc.ipcHashSha3_224.InputMessage:=Msg+DMEnc.Salt;
  DMEnc.ipcHashSha3_224.ComputeHash;
  if (Ord(DMEnc.ipcHashSha3_224.HashValue[13])>55)
  then DMEnc.ipcHashSha3_224.InputMessage:=DMEnc.ipcHashSha3_224.InputMessage+
       LeftStr(DMEnc.ipcHashSha3_224.HashValue,28)+DMEnc.Salt
  else DMEnc.ipcHashSha3_224.InputMessage:=DMEnc.ipcHashSha3_224.InputMessage+
       RightStr(DMEnc.ipcHashSha3_224.HashValue,28)+DMEnc.Salt;
  DMEnc.ipcHashSha3_224.ComputeHash;
  if (Ord(DMEnc.ipcHashSha3_224.HashValue[41])>55)
  then DMEnc.ipcHashSha3_224.InputMessage:=
    LeftStr(DMEnc.ipcHashSha3_224.HashValue,28)+
    DMEnc.ipcHashSha3_224.InputMessage+DMEnc.Salt
  else DMEnc.ipcHashSha3_224.InputMessage:=
    RightStr(DMEnc.ipcHashSha3_224.HashValue,28)+
    DMEnc.ipcHashSha3_224.InputMessage+DMEnc.Salt;
  DMEnc.ipcHashSha3_224.ComputeHash;
  Result:=DMEnc.ipcHashSha3_224.HashValue;
end;

function GetDirectHash256(const Text:string):string;
begin
  DMEnc.ipcHashSha3_256.InputMessage:= Text;
  DMEnc.ipcHashSha3_256.ComputeHash;
  Result:=DMEnc.ipcHashSha3_256.HashValue;
end;

function GetHash256(Msg:string):string;
var
  sBuf,sTemp:string;
begin
  DMEnc.ipcHashSha3_256.InputMessage:=Msg;
  DMEnc.ipcHashSha3_256.ComputeHash;
  sBuf:=DMEnc.ipcHashSha3_256.HashValue;
  if (Ord(sBuf[15])>55)
  then sTemp:= LeftStr(sBuf,32) else sTemp:= RightStr(sBuf,32);
  DMEnc.ipcHashSha3_256.InputMessage:=sBuf+sTemp;
  DMEnc.ipcHashSha3_256.ComputeHash;
  sBuf:=DMEnc.ipcHashSha3_256.HashValue;
  if (Ord(sBuf[31])>55)
  then sTemp:= LeftStr(sBuf,32) else sTemp:= RightStr(sBuf,32);
  DMEnc.ipcHashSha3_256.InputMessage:=sTemp+sBuf;
  DMEnc.ipcHashSha3_256.ComputeHash;
  sBuf:=DMEnc.ipcHashSha3_256.HashValue;
  if (Ord(sBuf[47])>55)
  then sTemp:= LeftStr(sBuf,32) else sTemp:= RightStr(sBuf,32);
  DMEnc.ipcHashSha3_256.InputMessage:=sBuf+sTemp;
  DMEnc.ipcHashSha3_256.ComputeHash;
  sBuf:=DMEnc.ipcHashSha3_256.HashValue;
  Result:=sBuf;
end;

procedure TDMEnc.DataModuleCreate(Sender: TObject);
begin
  _salt:= '';
end;

end.
