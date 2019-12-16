## Smartnote
A notepad you can use to store your private records.
By default you keep all your data locally, but you can also set up connection to remote storage.
It will protect you from data losing and make it easier to use application on different machines by synchronizing the data.

### Local storage 
Local storage is SQLite database protected by AES-256.
When you start application for the first time you enter passphrase which will used in key generation.
Application doesn't save key so there are no vulnerable comparison operations.
If you want to re-encrypt the local storage with a new key, please press appropriate button with a 'key' icon on the bottom panel.

### Remote storage
Remote storage is MySQL or MariaDB database. 
Press button 'Settings' if you want to setup connection to remote storage.
Application encrypts 'notes' on client side before send them to remote. 
Server side doesn't have the keys for decryption.
You can add 'salt' value to encryption keys if you use public server and want more unpredictability.
All data between client and remote storage transporting through secure SSH tunnel.
SSH password authentication available but it is highly recommended to use public keys.
