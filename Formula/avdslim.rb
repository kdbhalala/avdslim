class Avdslim < Formula
  desc "Cut Android emulator (AVD) host RAM from ~8.5 GB to ~2.5 GB on Apple Silicon & Linux"
  homepage "https://github.com/kdbhalala/avdslim"
  version "1.0.16"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/kdbhalala/avdslim/releases/download/v1.0.16/avdslim_v1.0.16_darwin_arm64.tar.gz"
      sha256 "26219bc445394b76e7fc866552f3180463bd1963f3dbe886055826fdeba4f480"
    else
      url "https://github.com/kdbhalala/avdslim/releases/download/v1.0.16/avdslim_v1.0.16_darwin_amd64.tar.gz"
      sha256 "27259176f91a60c8f752b25caf662fc76131821df4c528b43ce5d33f6a26e009"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/kdbhalala/avdslim/releases/download/v1.0.16/avdslim_v1.0.16_linux_arm64.tar.gz"
      sha256 "0e14228971bc289c53620260e0947f7f10cd748f93c7e27a2559de41a4c12d53"
    else
      url "https://github.com/kdbhalala/avdslim/releases/download/v1.0.16/avdslim_v1.0.16_linux_amd64.tar.gz"
      sha256 "dd800f047c2d041dbc7f9af59918fb4e4f311d802df4a4ee51eacfc4eec150aa"
    end
  end

  def install
    bin.install "avdslim"
  end

  test do
    assert_match "avdslim", shell_output("#{bin}/avdslim version")
  end
end
