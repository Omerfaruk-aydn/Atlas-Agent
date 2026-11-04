# Atlas Dots ekranı — karar notu

Tarih: 2026-11-03. Bu makine: Windows 10 Pro, derleme 19045.

## Denenenler

| Seçenek | Sonuç |
|---|---|
| Docker + Linux/Xvfb | `docker` komutu yok. Kurulmadı. |
| WSL2 Ubuntu (kurulu, sürüm 2) | Açılmıyor. Hata: `HCS_E_HYPERV_NOT_INSTALLED`. Sanallaştırma aygıt yazılımında kapalı. |
| Windows Sandbox | `WindowsSandbox.exe` var, `vmms` ve `vmcompute` hizmetleri çalışıyor. Pencere açılıyor ve şu metinle kapanıyor: "Hiper yönetici bulunamadı. Error 0xc0351000." 60 saniye içinde `vmwp.exe` hiç çıkmadı. |
| Hyper-V sanal makinesi | `Get-VM` yönetici izni istiyor. `HypervisorPresent=false`, `VirtualizationFirmwareEnabled=false`. Hiper yönetici olmadığı için bir VM de başlayamaz. Atlas zaten VM oluşturmuyor. |

CIM sonucu: `{"h":false,"v":false,"edition":"Professional","build":"19045"}`.

## Karar

Bu bilgisayarda konteyner veya sanal makine çalışmıyor. Donanım sanallaştırması (Intel VT-x / AMD-V) BIOS/UEFI'de açılıp Windows yeniden başlatılmadan Sandbox, WSL2 ve Hyper-V kullanılamaz. Atlas bu ayarı kendisi değiştirmez.

Uygulanan ortam: **dot başına ayrı bir headless Chrome**, profili `<workspace>/screen/profile` altında, hata ayıklama ucu yalnızca `127.0.0.1`. Kullanıcının Chrome profili kopyalanmaz. `computer` paketinin Win32 `SendInput` yolu bu çalışma alanında hiç çağrılmaz. Ekran görüntüsü ve tıklama CDP ile bu Chrome'a gider.

Bu bir işletim sistemi masaüstü değildir. Görev çubuğu, Not Defteri veya dosya gezgini yoktur. Sayfa açma, başlığı okuma ve tıklama vardır. Tam bir Linux masaüstü, sanallaştırma açıldıktan sonra ayrı bir iştir.

Tam yetki kapalıyken (`auto_accept_edits`) tarayıcıda gezinme ve bu ekrandaki tıklama/yazma onay ister. Tam yetki `bypass` olduğu için sormaz. Kullanıcı ekranı devraldığında dot'un değiştiren adımları reddedilir.
