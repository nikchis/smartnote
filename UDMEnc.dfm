object DMEnc: TDMEnc
  OldCreateOrder = False
  OnCreate = DataModuleCreate
  Height = 242
  Width = 375
  object ipcHashSha3_256: TipcHash
    Algorithm = haSHA3_256
    Left = 96
    Top = 128
  end
  object ipcHashSha3_224: TipcHash
    Algorithm = haSHA3_224
    Left = 96
    Top = 64
  end
  object ipcAESext: TipcAES
    UseHex = True
    Left = 191
    Top = 96
  end
end
