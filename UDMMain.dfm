object DMMain: TDMMain
  OldCreateOrder = False
  Height = 343
  Width = 813
  object dxSkinController: TdxSkinController
    NativeStyle = False
    SkinName = 'SharpPlus'
    Left = 72
    Top = 80
  end
  object dxLayoutLookAndFeelList: TdxLayoutLookAndFeelList
    Left = 72
    Top = 128
    object dxLayoutSkinLookAndFeel: TdxLayoutSkinLookAndFeel
      LookAndFeel.NativeStyle = False
      LookAndFeel.SkinName = 'SharpPlus'
    end
  end
  object LCLocal: TLiteConnection
    Options.Direct = True
    Options.EncryptionAlgorithm = leAES256
    Left = 328
    Top = 72
  end
  object ScSSHClient: TScSSHClient
    CiphersClient = <
      item
        Algorithm = saBlowfish
      end
      item
        Algorithm = saCast128
      end
      item
        Algorithm = saAES128
      end
      item
        Algorithm = saTripleDES
      end
      item
        Algorithm = saAES192
      end
      item
        Algorithm = saAES256
      end>
    CiphersServer = <
      item
        Algorithm = saBlowfish
      end
      item
        Algorithm = saCast128
      end
      item
        Algorithm = saAES128
      end
      item
        Algorithm = saTripleDES
      end
      item
        Algorithm = saAES256
      end
      item
        Algorithm = saAES192
      end>
    Timeout = 5
    Options.ServerAliveInterval = 10
    Options.MsgIgnoreRate = 5
    KeyStorage = ScRegStorage
    OnServerKeyValidate = ScSSHClientServerKeyValidate
    Left = 192
    Top = 80
  end
  object ScSSHChannel: TScSSHChannel
    Client = ScSSHClient
    SourcePort = 3308
    DestHost = '127.0.0.1'
    DestPort = 3306
    Timeout = 5
    Left = 192
    Top = 128
  end
  object UCMain: TUniConnection
    ProviderName = 'MySQL'
    Port = 3308
    Database = 'db_smartnote'
    SpecificOptions.Strings = (
      'MySQL.Charset=utf8'
      'MySQL.UseUnicode=True')
    Username = 'smartuser'
    Server = '127.0.0.1'
    LoginPrompt = False
    Left = 328
    Top = 200
  end
  object ScRegStorage: TScRegStorage
    Algorithm = saAES256
    Password = 'reg-storage-pass'
    Left = 192
    Top = 176
  end
  object cxEditRepository: TcxEditRepository
    Left = 72
    Top = 176
  end
  object LQNotes: TLiteQuery
    Connection = LCLocal
    SQL.Strings = (
      'SELECT notes.*, '
      'sq_note_hsts.note_id AS sq_note_id'
      'FROM notes'
      'LEFT JOIN'
      '  (SELECT note_id, COUNT(note_hst_id) AS count '
      '  FROM note_hsts GROUP BY note_id) '
      'AS sq_note_hsts USING(note_id)'
      'ORDER BY notes.name')
    Left = 416
    Top = 24
    object LQNotesnote_id: TIntegerField
      AutoGenerateValue = arAutoInc
      FieldName = 'note_id'
      Required = True
    end
    object LQNotesupdated: TDateTimeField
      FieldName = 'updated'
      Required = True
    end
    object LQNotesname: TStringField
      FieldName = 'name'
      Required = True
      Size = 64
    end
    object LQNotesinfo_hash: TStringField
      FieldName = 'info_hash'
      Required = True
      Size = 64
    end
    object LQNotessq_note_id: TIntegerField
      FieldName = 'sq_note_id'
      ReadOnly = True
    end
    object LQNotesinfo: TMemoField
      FieldName = 'info'
      BlobType = ftMemo
    end
  end
  object LDSNotes: TLiteDataSource
    DataSet = LQNotes
    Left = 416
    Top = 72
  end
  object LQNoteInsert: TLiteQuery
    Connection = LCLocal
    SQL.Strings = (
      'INSERT INTO notes '
      
        'VALUES(:note_id,datetime('#39'now'#39','#39'localtime'#39'),:name,:info,:info_ha' +
        'sh);')
    Left = 496
    Top = 24
    ParamData = <
      item
        DataType = ftUnknown
        Name = 'note_id'
        Value = nil
      end
      item
        DataType = ftUnknown
        Name = 'name'
        Value = nil
      end
      item
        DataType = ftUnknown
        Name = 'info'
        Value = nil
      end
      item
        DataType = ftUnknown
        Name = 'info_hash'
        Value = nil
      end>
  end
  object LQNoteId: TLiteQuery
    Connection = LCLocal
    SQL.Strings = (
      'SELECT note_id FROM notes ORDER BY note_id DESC LIMIT 1')
    Left = 496
    Top = 72
    object LQNoteIdnote_id: TIntegerField
      FieldName = 'note_id'
      Required = True
    end
  end
  object LQNoteUpdate: TLiteQuery
    Connection = LCLocal
    SQL.Strings = (
      'UPDATE notes '
      'SET name = :name, info = :info, '
      'info_hash = :info_hash, updated = datetime('#39'now'#39','#39'localtime'#39')'
      'WHERE note_id = :note_id')
    Left = 576
    Top = 24
    ParamData = <
      item
        DataType = ftUnknown
        Name = 'name'
        Value = nil
      end
      item
        DataType = ftUnknown
        Name = 'info'
        Value = nil
      end
      item
        DataType = ftUnknown
        Name = 'info_hash'
        Value = nil
      end
      item
        DataType = ftUnknown
        Name = 'note_id'
        Value = nil
      end>
  end
  object LQNoteDelete: TLiteQuery
    Connection = LCLocal
    SQL.Strings = (
      'DELETE FROM notes WHERE note_id = :note_id')
    Left = 576
    Top = 72
    ParamData = <
      item
        DataType = ftUnknown
        Name = 'note_id'
        Value = nil
      end>
  end
  object LQNoteHstInsert: TLiteQuery
    Connection = LCLocal
    SQL.Strings = (
      'INSERT INTO note_hsts '
      'VALUES(:note_hst_id,:note_id,:created,:info,:info_hash);')
    Left = 664
    Top = 72
    ParamData = <
      item
        DataType = ftUnknown
        Name = 'note_hst_id'
        Value = nil
      end
      item
        DataType = ftUnknown
        Name = 'note_id'
        Value = nil
      end
      item
        DataType = ftUnknown
        Name = 'created'
        Value = nil
      end
      item
        DataType = ftUnknown
        Name = 'info'
        Value = nil
      end
      item
        DataType = ftUnknown
        Name = 'info_hash'
        Value = nil
      end>
  end
  object LQNoteHstId: TLiteQuery
    Connection = LCLocal
    SQL.Strings = (
      
        'SELECT note_hst_id FROM note_hsts ORDER BY note_hst_id DESC LIMI' +
        'T 1')
    Left = 664
    Top = 24
    object LQNoteHstIdnote_hst_id: TIntegerField
      FieldName = 'note_hst_id'
      Required = True
    end
  end
  object MySQLUniProvider: TMySQLUniProvider
    Left = 136
    Top = 240
  end
  object UQNotes: TUniQuery
    Connection = UCMain
    SQL.Strings = (
      'SELECT notes.* FROM notes')
    Left = 416
    Top = 200
    object UQNotesnote_id: TIntegerField
      FieldName = 'note_id'
      Required = True
    end
    object UQNotesupdated: TDateTimeField
      FieldName = 'updated'
      Required = True
    end
    object UQNotesname: TWideStringField
      FieldName = 'name'
      Required = True
      Size = 64
    end
    object UQNotesinfo_hash: TWideStringField
      FieldName = 'info_hash'
      Required = True
      Size = 64
    end
    object UQNotesinfo: TWideMemoField
      FieldName = 'info'
      Required = True
      BlobType = ftWideMemo
    end
  end
  object UQNoteUpdate: TUniQuery
    Connection = UCMain
    SQL.Strings = (
      'UPDATE notes '
      'SET name = :name, info = :info, '
      'info_hash = :info_hash, updated = :updated'
      'WHERE note_id = :note_id')
    Left = 576
    Top = 200
    ParamData = <
      item
        DataType = ftUnknown
        Name = 'name'
        Value = nil
      end
      item
        DataType = ftUnknown
        Name = 'info'
        Value = nil
      end
      item
        DataType = ftUnknown
        Name = 'info_hash'
        Value = nil
      end
      item
        DataType = ftUnknown
        Name = 'updated'
        Value = nil
      end
      item
        DataType = ftUnknown
        Name = 'note_id'
        Value = nil
      end>
  end
  object UQNoteDelete: TUniQuery
    Connection = UCMain
    SQL.Strings = (
      'DELETE FROM notes WHERE note_id = :note_id')
    Left = 496
    Top = 168
    ParamData = <
      item
        DataType = ftUnknown
        Name = 'note_id'
        Value = nil
      end>
    object IntegerField2: TIntegerField
      FieldName = 'note_id'
      Required = True
    end
    object DateTimeField2: TDateTimeField
      FieldName = 'updated'
      Required = True
    end
    object WideStringField4: TWideStringField
      FieldName = 'name'
      Required = True
      Size = 64
    end
    object WideStringField5: TWideStringField
      FieldName = 'info'
      Required = True
      Size = 2048
    end
    object WideStringField6: TWideStringField
      FieldName = 'info_hash'
      Required = True
      Size = 64
    end
  end
  object UQNoteInsert: TUniQuery
    Connection = UCMain
    SQL.Strings = (
      'INSERT INTO notes '
      'VALUES(:note_id,:updated,:name,:info,:info_hash);')
    Left = 496
    Top = 232
    ParamData = <
      item
        DataType = ftUnknown
        Name = 'note_id'
        Value = nil
      end
      item
        DataType = ftUnknown
        Name = 'updated'
        Value = nil
      end
      item
        DataType = ftUnknown
        Name = 'name'
        Value = nil
      end
      item
        DataType = ftUnknown
        Name = 'info'
        Value = nil
      end
      item
        DataType = ftUnknown
        Name = 'info_hash'
        Value = nil
      end>
    object IntegerField1: TIntegerField
      FieldName = 'note_id'
      Required = True
    end
    object DateTimeField1: TDateTimeField
      FieldName = 'updated'
      Required = True
    end
    object WideStringField1: TWideStringField
      FieldName = 'name'
      Required = True
      Size = 64
    end
    object WideStringField2: TWideStringField
      FieldName = 'info'
      Required = True
      Size = 2048
    end
    object WideStringField3: TWideStringField
      FieldName = 'info_hash'
      Required = True
      Size = 64
    end
  end
  object LQNetwork: TLiteQuery
    Connection = LCLocal
    SQL.Strings = (
      'SELECT networks.* FROM networks LIMIT 1')
    Left = 744
    Top = 48
    object LQNetworknetwork_id: TSmallintField
      FieldName = 'network_id'
      Required = True
    end
    object LQNetworkactive: TBooleanField
      FieldName = 'active'
      Required = True
    end
    object LQNetworkssh_host: TStringField
      FieldName = 'ssh_host'
      Size = 64
    end
    object LQNetworkssh_port: TIntegerField
      FieldName = 'ssh_port'
    end
    object LQNetworkssh_user: TStringField
      FieldName = 'ssh_user'
      Size = 64
    end
    object LQNetworkssh_pwd: TStringField
      FieldName = 'ssh_pwd'
      Size = 64
    end
    object LQNetworkdb_lc_port: TIntegerField
      FieldName = 'db_lc_port'
    end
    object LQNetworkdb_fw_host: TStringField
      FieldName = 'db_fw_host'
      Size = 64
    end
    object LQNetworkdb_fw_port: TIntegerField
      FieldName = 'db_fw_port'
    end
    object LQNetworkdb_name: TStringField
      FieldName = 'db_name'
      Size = 64
    end
    object LQNetworkdb_user: TStringField
      FieldName = 'db_user'
      Size = 64
    end
    object LQNetworkdb_pwd: TStringField
      FieldName = 'db_pwd'
      Size = 64
    end
    object LQNetworksalt: TStringField
      FieldName = 'salt'
      Size = 64
    end
    object LQNetworkts: TIntegerField
      FieldName = 'ts'
    end
  end
  object UniSQLNotes: TUniSQL
    Connection = UCMain
    SQL.Strings = (
      'CREATE TABLE notes ('
      '  note_id INTEGER NOT NULL PRIMARY KEY,'
      '  updated DATETIME NOT NULL,'
      '  name VARCHAR(64) NOT NULL,'
      '  info TEXT NOT NULL,'
      '  info_hash VARCHAR(64) NOT NULL'
      ');'
      'CREATE TABLE last_sync ('
      '  ts INTEGER NOT NULL'
      ');'
      'INSERT INTO last_sync(ts) VALUES(0);')
    Left = 664
    Top = 200
  end
  object LDLocal: TLiteDump
    SQL.Strings = (
      'CREATE TABLE [networks] ('
      '  [network_id] TINYINT NOT NULL PRIMARY KEY,'
      '  [active] BOOLEAN NOT NULL,'
      '  [ssh_host] VARCHAR(64),'
      '  [ssh_port] INTEGER,'
      '  [ssh_user] VARCHAR(64),'
      '  [ssh_pwd] VARCHAR(64),'
      '  [db_lc_port] INTEGER,'
      '  [db_fw_host] VARCHAR(64),'
      '  [db_fw_port] INTEGER,'
      '  [db_name] VARCHAR(64),'
      '  [db_user] VARCHAR(64),'
      '  [db_pwd] VARCHAR(64),'
      '  [salt] VARCHAR(64),'
      '  [ts] INTEGER NOT NULL DEFAULT 0'
      ');'
      'INSERT INTO networks(network_id,active,ts) VALUES (1,0,0);'
      'CREATE TABLE [notes] ('
      '  [note_id] INTEGER NOT NULL PRIMARY KEY,'
      '  [updated] DATETIME NOT NULL,'
      '  [name] VARCHAR(64) NOT NULL,'
      '  [info] TEXT NOT NULL,'
      '  [info_hash] VARCHAR(64) NOT NULL'
      ');'
      'CREATE TABLE [note_hsts] ('
      '  [note_hst_id] INTEGER NOT NULL PRIMARY KEY,'
      
        '  [note_id] INTEGER NOT NULL REFERENCES [notes]([note_id]) ON DE' +
        'LETE CASCADE,'
      '  [created] DATETIME NOT NULL,'
      '  [info] TEXT NOT NULL,'
      '  [info_hash] VARCHAR(64) NOT NULL'
      ');')
    Connection = LCLocal
    Left = 328
    Top = 24
  end
  object UQCurTs: TUniQuery
    Connection = UCMain
    SQL.Strings = (
      'SELECT UNIX_TIMESTAMP() AS ts;')
    Left = 744
    Top = 200
    object UQCurTsts: TLargeintField
      FieldName = 'ts'
    end
  end
  object UQLastSync: TUniQuery
    Connection = UCMain
    SQL.Strings = (
      'SELECT ts FROM last_sync ORDER BY ts DESC LIMIT 1;')
    Left = 600
    Top = 280
    object UQLastSyncts: TIntegerField
      FieldName = 'ts'
    end
  end
  object UQLastSyncUpdate: TUniQuery
    Connection = UCMain
    SQL.Strings = (
      'UPDATE last_sync SET ts = :ts;')
    Left = 684
    Top = 280
    ParamData = <
      item
        DataType = ftUnknown
        Name = 'ts'
        Value = nil
      end>
  end
end
